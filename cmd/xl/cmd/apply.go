package cmd

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/deckarep/golang-set"
	"github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/thoas/go-funk"
	"github.com/xebialabs/xl-cli/pkg/models"
	"github.com/xebialabs/xl-cli/pkg/util"
	"github.com/xebialabs/xl-cli/pkg/xl"
)

type FileWithDocuments struct {
	imports   []string
	parent    *string
	documents []*xl.Document
	fileName  string
}

var applyFilenames []string
var applyValues map[string]string

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply configuration changes",
	Long:  `Apply configuration changes`,
	Run: func(cmd *cobra.Command, args []string) {
		DoApply(cmd, applyFilenames)
	},
}

func printIds(op string, ids *[]string) {
	if ids != nil && len(*ids) > 0 {
		for _, id := range *ids {
			util.Info(fmt.Sprintf("%s %s\n", op, id))
		}
	}
}

func printChangedIds(entityName string, ids *xl.ChangedIds) {
	if ids != nil {
		printIds(fmt.Sprintf("Created %s:", entityName), ids.Created)
		printIds(fmt.Sprintf("Updated %s:", entityName), ids.Updated)
	}
}

func printTaskInfo(task *xl.TaskInfo) {
	if task != nil {
		util.Info(fmt.Sprintf("Task [%s] started (%s)\n", task.Description, task.Id))
	}
}

func printChanges(changes *xl.Changes) {
	if changes != nil {
		printTaskInfo(changes.Task)
		printChangedIds("ci", changes.Cis)
		printChangedIds("user", changes.Users)
		printChangedIds("permission", changes.Permissions)
		printChangedIds("role", changes.Roles)
	}
}

func checkForEmptyImport(importedFile string) {
	if len(strings.TrimSpace(importedFile)) == 0 {
		util.Fatal("The 'imports' field contains empty elements.\n")
	}
}

func extractImports(baseDir string, doc *xl.Document) []string {
	if doc.Metadata != nil && doc.Metadata["imports"] != nil {
		if imports, ok := doc.Metadata["imports"].([]interface{}); !ok {
			util.Fatal("The 'imports' field has wrong format. Must be a list of strings.\n")
		} else {
			delete(doc.Metadata, "imports")
			importedFiles, _ := funk.Map(imports, func(i interface{}) string {
				importedFile, _ := i.(string)
				checkForEmptyImport(importedFile)
				err := util.ValidateFilePath(importedFile, "imports")
				if err != nil {
					util.Fatal(err.Error())
				}
				return filepath.Join(baseDir, filepath.FromSlash(importedFile))
			}).([]string)
			return importedFiles
		}
	}
	return make([]string, 0)
}

func readDocumentsFromFile(fileName string, parent *string) FileWithDocuments {
	reader, err := os.Open(fileName)
	if err != nil {
		util.Fatal("Error while opening XL YAML file %s: %s\n", fileName, err)
	}
	imports := make([]string, 0)
	documents := make([]*xl.Document, 0)
	docReader := xl.NewDocumentReader(reader)
	baseDir := util.AbsoluteFileDir(fileName)
	util.Verbose("Reading file: %s, base dir: %s\n", fileName, baseDir)
	for {
		doc, err := docReader.ReadNextYamlDocument()
		if err != nil {
			if err == io.EOF {
				break
			} else {
				reportFatalDocumentError(fileName, doc, err)
			}
		}
		imports = append(imports, extractImports(baseDir, doc)...)
		documents = append(documents, doc)
	}
	reader.Close()
	return FileWithDocuments{imports, parent, documents, fileName}
}

func validateFileWithDocs(filesWithDocs []FileWithDocuments) {
	funk.ForEach(filesWithDocs, func(file FileWithDocuments) {
		funk.ForEach(file.documents, func(doc *xl.Document) {
			if doc.Kind == models.ImportSpecKind && doc.ApiVersion != models.YamlFormatVersion {
				util.Fatal("unknown apiVersion for %s spec kind: %s\n", models.ImportSpecKind, doc.ApiVersion)
			}
		})
	})
}

func parseDocuments(fileNames []string, seenFiles mapset.Set, parent *string) []FileWithDocuments {
	result := make([]FileWithDocuments, 0)
	for _, fileName := range fileNames {
		if !seenFiles.Contains(fileName) {
			fileWithDocuments := readDocumentsFromFile(fileName, parent)
			result = append(result, fileWithDocuments)
			seenFiles.Add(fileName)
			result = append(parseDocuments(fileWithDocuments.imports, seenFiles, &fileName), result...)
		}
	}
	validateFileWithDocs(result)
	return result
}

func requestTaskId(context *xl.Context, doc *xl.Document, taskId string) (*xl.TaskState, error) {
	server, err := context.GetDocumentHandlingServer(doc)
	if err != nil {
		return nil, err
	}

	util.Verbose("Checking task state... ")
	state, serr := server.GetTaskStatus(taskId)
	if serr != nil {
		return nil, serr
	}
	util.Verbose("[%s]\n", state.State)

	if util.IsVerbose {
		if len(state.CurrentSteps) > 0 {
			util.Verbose("### Currently active task steps:\n")
			for _, step := range state.CurrentSteps {
				util.Verbose("### %s [%s]\n", step.Name, step.State)
			}
		}
	} else {
		util.Info(".")
	}
	return state, nil
}

func waitForTasks(context *xl.Context, doc *xl.Document, changes *xl.Changes) {
	if changes != nil && changes.Task != nil {
		util.Info("Waiting for task (%s)\n", changes.Task.Id)
		result, err := requestTaskId(context, doc, changes.Task.Id)
		for err == nil {
			switch result.State {
			case "COMPLETED":
				fallthrough
			case "DONE":
				util.Verbose("Done.")
				util.Info("\n")
				return

			case "IN_PROGRESS":
				for _, step := range result.CurrentSteps {
					if !step.Automated {
						util.Fatal("\nUnable to complete the task (%s) automatically as it's current active step is manual.\n", changes.Task.Id)
					}
				}

			case "FAILING":
				fallthrough
			case "CANCELING":
				fallthrough
			case "CANCELLED":
				fallthrough
			case "FAILED":
				fallthrough
			case "STOPPED":
				fallthrough
			case "ABORTED":
				util.Fatal("\nUnable to complete the task (%s) automatically as it's state became [%s]. The task will be rolled back.\n", changes.Task.Id, result.State)
			}
			time.Sleep(2 * time.Second)
			result, err = requestTaskId(context, doc, changes.Task.Id)
		}
		if err != nil {
			util.Fatal("\nError waiting for task %s, %s\n", changes.Task.Id, err)
		}
	}
}

func DoApply(cmd *cobra.Command, applyFilenames []string) {
	homeValsFiles, e := listHomeXlValsFiles()

	if e != nil {
		util.Fatal("Error while reading value files from home: %s\n", e)
	}

	docs := parseDocuments(util.ToAbsolutePaths(applyFilenames), mapset.NewSet(), nil)

	util.VerboseSeparator()
	for _, fileWithDocs := range docs {

		var applyFile = util.PrintableFileName(fileWithDocs.fileName)
		if fileWithDocs.parent != nil {
			var parentFile = util.PrintableFileName(*fileWithDocs.parent)
			util.Info("Applying %s (imported by %s)\n", applyFile, parentFile)
		} else {
			util.Info("Applying %s\n", applyFile)
		}

		projectValsFiles, err := listRelativeXlValsFiles(filepath.Dir(fileWithDocs.fileName))
		if err != nil {
			util.Fatal("Error while reading value files for %s from project: %s\n", fileWithDocs.fileName, err)
		}

		allValsFiles := append(homeValsFiles, projectValsFiles...)

		context, err := xl.BuildContext(viper.GetViper(), &applyValues, allValsFiles)
		if err != nil {
			util.Fatal("Error while reading configuration: %s\n", err)
		}

		applyDir := filepath.Dir(fileWithDocs.fileName)

		for _, doc := range fileWithDocs.documents {
			util.Verbose("---\n")
			util.Verbose("Applying document at line %d\n", doc.Line)
			if doc.Kind != models.ImportSpecKind {
				changes, err := context.ProcessSingleDocument(doc, applyDir)
				printChanges(changes)
				waitForTasks(context, doc, changes)
				if err != nil {
					reportFatalDocumentError(fileWithDocs.fileName, doc, err)
				}
			} else {
				util.Info("Done\n")
			}
		}
		util.VerboseSeparator()
	}
}

var isFieldAlreadySetErrorRegexp = regexp.MustCompile(`field \w+ already set in type`)

func reportFatalDocumentError(applyFilename string, doc *xl.Document, err error) {
	if isFieldAlreadySetErrorRegexp.MatchString(err.Error()) {
		err = errors.Wrap(err, "Possible missing triple dash (---) to separate multiple YAML documents")
	}

	util.Fatal("Error while processing YAML document at line %d of XL YAML file %s: %s\n", doc.Line, applyFilename, err)
}

func init() {
	rootCmd.AddCommand(applyCmd)

	applyFlags := applyCmd.Flags()
	applyFlags.StringArrayVarP(&applyFilenames, "file", "f", []string{}, "Path(s) to the file(s) to apply (required)")
	_ = applyCmd.MarkFlagRequired("file")
	applyFlags.StringToStringVar(&applyValues, "values", map[string]string{}, "Values")
}

func listHomeXlValsFiles() ([]string, error) {
	home, err := homedir.Dir()
	if err != nil {
		return nil, err
	}
	xebialabsFolder := path.Join(home, ".xebialabs")
	if _, err := os.Stat(xebialabsFolder); os.IsNotExist(err) {
		return []string{}, nil
	}
	valfiles, err := util.FindByExtInDirSorted(xebialabsFolder, ".xlvals")
	if err != nil {
		return nil, err
	}
	return valfiles, nil
}

func listRelativeXlValsFiles(dir string) ([]string, error) {
	valfiles, err := util.FindByExtInDirSorted(dir, ".xlvals")
	if err != nil {
		return nil, err
	}
	return valfiles, nil
}
