package cmd

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/xebialabs/xl-cli/pkg/xl"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"path"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/viper"
)

var applyFilenames []string
var applyValues map[string]string

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply configuration changes",
	Long:  `Apply configuration changes`,
	Run: func(cmd *cobra.Command, args []string) {
		DoApply(applyFilenames)
	},
}

func printCiIds(kind string, ids *[]string) {
	if ids != nil && len(*ids) > 0 {
		xl.Verbose("...... ---------------\n")
		xl.Verbose(fmt.Sprintf("...... %s CIs:\n", kind))
		xl.Verbose("...... ---------------\n")
		for idx, id := range *ids {
			xl.Verbose(fmt.Sprintf("...... %d. %s\n", idx+1, id))
		}
		xl.Verbose("...... ---------------\n")
	}
}

func printChangedCis(changedCis *xl.ChangedCis) {
	if changedCis != nil {
		printCiIds("Created", changedCis.Created)
		printCiIds("Updated", changedCis.Updated)
	}
}

func printTaskInfo(task *xl.TaskInfo) {
	if task != nil {
		xl.Verbose("...... ---------------\n")
		xl.Verbose(fmt.Sprintf("...... Task [%s] is started:\n", task.Id))
		xl.Verbose(fmt.Sprintf("...... %s.\n", task.Description))
		xl.Verbose("...... ---------------\n")
	}
}

func printChanges(changes *xl.Changes) {
	if changes != nil {
		printChangedCis(changes.Cis)
		printTaskInfo(changes.Task)
	}
}

func DoApply(applyFilenames []string) {

	homeValsFiles, e := listHomeXlValsFiles()

	if e != nil {
		xl.Fatal("Error while reading value files from home: %s\n", e)
	}

	for _, applyFilename := range applyFilenames {

		projectValsFiles, err := listRelativeXlValsFiles(filepath.Dir(applyFilename))
		if err != nil {
			xl.Fatal("Error while reading value files for %s from project: %s\n", applyFilename, err)
		}

		allValsFiles := append(homeValsFiles, projectValsFiles...)

		context, err := xl.BuildContext(viper.GetViper(), &applyValues, allValsFiles)
		if err != nil {
			xl.Fatal("Error while reading configuration: %s\n", err)
		}
		if xl.IsVerbose {
			xl.Info("Context for document %s\n", applyFilename)
			context.PrintConfiguration()
		}

		xl.StartProgress(applyFilename)

		applyDir := filepath.Dir(applyFilename)
		reader, err := os.Open(applyFilename)
		if err != nil {
			xl.Fatal("Error while opening XL YAML file %s: %s\n", applyFilename, err)
		}

		docReader := xl.NewDocumentReader(reader)
		for {
			doc, err := docReader.ReadNextYamlDocument()
			if err != nil {
				if err == io.EOF {
					break
				} else {
					reportFatalDocumentError(applyFilename, doc, err)
				}
			}

			xl.UpdateProgressStartDocument(applyFilename, doc)
			changes, err := context.ProcessSingleDocument(doc, applyDir)
			printChanges(changes)
			if err != nil {
				reportFatalDocumentError(applyFilename, doc, err)
			}
			xl.UpdateProgressEndDocument()
		}

		reader.Close()
		xl.EndProgress()
	}

}

var isFieldAlreadySetErrorRegexp = regexp.MustCompile(`field \w+ already set in type`)

func reportFatalDocumentError(applyFilename string, doc *xl.Document, err error) {
	if isFieldAlreadySetErrorRegexp.MatchString(err.Error()) {
		err = errors.Wrap(err, "Possible missing triple dash (---) to separate multiple YAML documents")
	}

	xl.Fatal("Error while processing YAML document at line %d of XL YAML file %s: %s\n", doc.Line, applyFilename, err)
}

func init() {
	rootCmd.AddCommand(applyCmd)

	applyFlags := applyCmd.Flags()
	applyFlags.StringArrayVarP(&applyFilenames, "file", "f", []string{}, "Path(s) to the file(s) to apply (required)")
	applyCmd.MarkFlagRequired("file")
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
	valfiles, err := xl.FindByExtInDirSorted(xebialabsFolder, ".xlvals")
	if err != nil {
		return nil, err
	}
	return valfiles, nil
}

func listRelativeXlValsFiles(dir string) ([]string, error) {
	valfiles, err := xl.FindByExtInDirSorted(dir, ".xlvals")
	if err != nil {
		return nil, err
	}
	return valfiles, nil
}