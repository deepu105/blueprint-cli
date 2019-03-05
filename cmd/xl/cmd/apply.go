package cmd

import (
	"fmt"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"github.com/xebialabs/xl-cli/pkg/models"
	"github.com/xebialabs/xl-cli/pkg/util"
	"github.com/xebialabs/xl-cli/pkg/xl"
	"os"
	"path/filepath"
	"time"
)

var applyFilenames []string
var applyValues map[string]string
var applyDetach bool
var nonInteractive bool

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply configuration changes",
	Long:  `Apply configuration changes`,
	Run: func(cmd *cobra.Command, args []string) {
		DoApply(applyFilenames)
	},
}

func printIds(op string, ids *[]string) {
	if ids != nil && len(*ids) > 0 {
		for _, id := range *ids {
			util.Info(fmt.Sprintf("%s%s %s\n", util.IndentFlexible(), op, id))
		}
	}
}

func printChangedIds(entityName string, ids *xl.ChangedIds) {
	if ids != nil {
		printIds(fmt.Sprintf("Created %s", entityName), ids.Created)
		printIds(fmt.Sprintf("Updated %s", entityName), ids.Updated)
	}
}

func printTaskInfo(task *xl.TaskInfo) {
	if task != nil {
		util.Info("%s%s started\n", util.IndentFlexible(), task.Description)
	}
}

func printChanges(changes *xl.Changes) {
	if changes != nil {
		printTaskInfo(changes.Task)
		printChangedIds("CI", changes.Cis)
		printChangedIds("user", changes.Users)
		printChangedIds("permissions for role", changes.Permissions)
		printChangedIds("role", changes.Roles)
	}
}

func requestTaskId(context *xl.Context, doc *xl.Document, taskId string) (*xl.TaskState, error) {
	server, err := context.GetDocumentHandlingServer(doc)
	if err != nil {
		return nil, err
	}

	util.Verbose("%sChecking task state... ", util.Indent2())
	state, serr := server.GetTaskStatus(taskId)
	if serr != nil {
		return nil, serr
	}
	util.Verbose("%s\n", state.State)

	if util.IsVerbose {
		if len(state.CurrentSteps) > 0 {
			util.Verbose("%sCurrently active task steps:\n", util.Indent2())
			for _, step := range state.CurrentSteps {
				util.Verbose("%s%s %s\n", util.Indent3(), step.Name, step.State)
			}
		}
	} else {
		util.Info(".")
	}
	return state, nil
}

func waitForTasks(context *xl.Context, doc *xl.Document, changes *xl.Changes, shouldDetach bool) {
	if changes != nil && changes.Task != nil {
		if shouldDetach {
			util.Info("%sGo to the user interface to follow task %s\n", util.IndentFlexible(), changes.Task.Id)
		} else {
			util.Info("%sWaiting for task %s to finish\n", util.Indent1(), changes.Task.Id)
			if !util.IsVerbose {
				util.Info(util.Indent1())
			}
			result, err := requestTaskId(context, doc, changes.Task.Id)
			for err == nil {
				switch result.State {
				case "COMPLETED":
					fallthrough
				case "EXECUTED":
					fallthrough
				case "DONE":
					if !util.IsVerbose {
						util.Info("\n")
					}
					util.Info("%sTask %s has completed and been archived\n", util.Indent1(), changes.Task.Id)
					return

				case "IN_PROGRESS":
					for _, step := range result.CurrentSteps {
						if !step.Automated {
							util.Fatal(
								"\n%sUnable to complete the task (%s) automatically as it's current active step is manual.\n",
								util.Indent1(), changes.Task.Id,
							)
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
					util.Fatal(
						"\n%sUnable to complete the task %s automatically as it's state became %s.\n%sThe task will be rolled back.\n",
						util.Indent1(), changes.Task.Id, result.State, util.Indent1(),
					)
				}
				time.Sleep(2 * time.Second)
				util.Verbose("\n")
				result, err = requestTaskId(context, doc, changes.Task.Id)
			}
			if err != nil {
				util.Fatal("\n%sError waiting for task %s, %s\n", util.Indent1(), changes.Task.Id, err)
			}
		}
	}
}

func fillInOnSuccessPolicy(specMap map[interface{}]interface{}) {
	isNotTty := !isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsCygwinTerminal(os.Stdout.Fd())
	if isNotTty || nonInteractive {
		specMap["onSuccessPolicy"] = "ARCHIVE"
	}
}

func fillInTaskPolicies(doc *xl.Document) {
	if doc.Kind == models.DeploymentSpecKind {
		if specMap, ok := doc.Spec.(map[interface{}]interface{}); ok {
			fillInOnSuccessPolicy(specMap)
		}
	}
}

func applyDocument(context *xl.Context, fileWithDocs xl.FileWithDocuments, doc *xl.Document) {
	fillInTaskPolicies(doc)
	applyDir := filepath.Dir(fileWithDocs.FileName)
	changes, err := context.ProcessSingleDocument(doc, applyDir)
	printChanges(changes)
	waitForTasks(context, doc, changes, applyDetach)
	if err != nil {
		xl.ReportFatalDocumentError(fileWithDocs.FileName, doc, err)
	}
}

func DoApply(applyFilenames []string) {
	xl.ForEachDocument("Applying", applyFilenames, applyValues, applyDocument)
}

func init() {
	rootCmd.AddCommand(applyCmd)

	applyFlags := applyCmd.Flags()
	applyFlags.StringArrayVarP(&applyFilenames, "file", "f", []string{}, "Path(s) to the file(s) to apply (required)")
	_ = applyCmd.MarkFlagRequired("file")
	applyFlags.StringToStringVar(&applyValues, "values", map[string]string{}, "Values")
	applyFlags.BoolVarP(&applyDetach, "detach", "d", false, "Detach the client at the moment of starting a deploy or release")
	applyFlags.BoolVar(&nonInteractive, "non-interactive", false, "Automatically archive finished deployment tasks")
}
