package util

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"

	"github.com/briandowns/spinner"
	"github.com/xebialabs/xl-cli/pkg/models"
	"github.com/xebialabs/xl-cli/pkg/osSpecific"
	"gopkg.in/AlecAivazis/survey.v1"
)

const (
	DEPLOYING = iota
	UNDEPLOYING
	UPDATING
	UNKNOWN
)

var currentTask = ""
var ctDesc = ""

var phase = UNKNOWN

// TODO a better way or to use the APIs available
var generatedPlan = "c.x.d.s.deployment.DeploymentService - Generated plan"
var phaseLogEnd = "on K8S"
var executedLog = "is completed with state [DONE]"
var failExecutedLog = "is completed with state [FAILED]"

func identifyPhase(log string) (phase int, start int) {
	switch {
	case strings.Contains(log, "# [Serial] Deploy"):
		return DEPLOYING, getIndexPlusLen(log, "# [Serial] Deploy")
	case strings.Contains(log, "* Deploy"):
		return DEPLOYING, getIndexPlusLen(log, "* Deploy")
	case strings.Contains(log, "# [Serial] Update"):
		return UPDATING, getIndexPlusLen(log, "# [Serial] Update")
	case strings.Contains(log, "* Update"):
		return UPDATING, getIndexPlusLen(log, "* Update")
	case strings.Contains(log, "# [Serial] Undeploy"):
		return UNDEPLOYING, getIndexPlusLen(log, "# [Serial] Undeploy")
	case strings.Contains(log, "* Undeploy"):
		return UNDEPLOYING, getIndexPlusLen(log, "* Undeploy")
	default:
		return UNKNOWN, -1
	}
}

func logCapture(w io.Writer, d []byte, s *spinner.Spinner) {
	eventLog := string(d)

	if strings.Index(eventLog, generatedPlan) != -1 {
		currentTask = getCurrentTask(eventLog, strings.Index(eventLog, generatedPlan))
		if currentTask != "" {
			phase2, start := identifyPhase(eventLog)
			phase = phase2
			end := strings.Index(eventLog, phaseLogEnd)

			if start >= 0 && end >= 0 {
				ctDesc = eventLog[start:end]
				write(getCurrentStage(false, phase), s, w)
			}
		}
	}

	if strings.Index(eventLog, failExecutedLog) != -1 {
		if phase == DEPLOYING || phase == UPDATING {
			write("Failed deploying for ", s, w)
			phase = UNDEPLOYING
			write(getCurrentStage(false, phase), s, w)
		} else {
			write("Failed undeploying for ", s, w)
		}
	}

	if strings.Index(eventLog, executedLog) != -1 {
		write(getCurrentStage(true, phase), s, w)
	}
}

func getCurrentStage(isExecuted bool, phase int) string {
	var currentStage string

	switch phase {
	case DEPLOYING:
		currentStage = "Deploy"
	case UNDEPLOYING:
		currentStage = "Undeploy"
	case UPDATING:
		currentStage = "Updat" // isExecuted appends ed/ing
	default:
		currentStage = "Finish"
	}

	if isExecuted {
		currentStage += "ed"
	} else {
		currentStage += "ing"
	}

	return currentStage
}

var lastWritten = ""

func write(currentStage string, s *spinner.Spinner, w io.Writer) {
	if phase == UNKNOWN || currentStage == lastWritten {
		return
	}

	if ctDesc != "" {
		lastWritten = currentStage

		s.Stop()
		w.Write([]byte(currentStage + ctDesc + "\n\n"))
		s.Start()
	}
}

func getIndexPlusLen(eventLog string, ident string) int {
	index := strings.Index(eventLog, ident)
	if index >= 0 {
		return index + len(ident)
	}
	return index
}

func getCurrentTask(eventLog string, index int) string {
	start := index + len(generatedPlan)
	end := strings.Index(eventLog, "\n")

	if end > 0 && start > 0 {
		task := eventLog[start:end]
		words := strings.Split(task, " ")

		for _, word := range words {
			char := strings.Split(word, "-")
			if len(char) > 1 {
				return word
			}
		}
	}
	return ""
}

func copyAndCapture(w io.Writer, r io.Reader, s *spinner.Spinner) ([]byte, error) {
	var out []byte
	buf := make([]byte, 1024, 1024)
	for {
		n, err := r.Read(buf[:])
		if err != nil {
			// Read returns io.EOF at the end of file, which is not an error for us
			if err == io.EOF {
				err = nil
			}
			// since it EOF we return here
			return out, err
		}
		if n > 0 {
			d := buf[:n]
			out = append(out, d...)

			if IsVerbose {
				_, err := w.Write(d)
				if err != nil {
					return out, err
				}
			}

			logCapture(w, d, s)
		}
	}
}

func ExecuteCommandAndShowLogs(command models.Command, s *spinner.Spinner) (string, string, error) {
	cmd := exec.Command(command.Name, command.Args...)
	if !IsVerbose {
		s.Start()
	}

	cmd.SysProcAttr = osSpecific.GetSyscall()

	var stdout, stderr []byte
	var errStdout, errStderr error

	stdoutIn, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", err
	}
	stderrIn, err := cmd.StderrPipe()
	if err != nil {
		return "", "", err
	}
	err = cmd.Start()
	if err != nil {
		return "", "", fmt.Errorf("cmd.Start() failed with '%s'", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)

	done := make(chan bool)
	sig := make(chan os.Signal)
	signal.Notify(sig, os.Interrupt)

	go func() {
		for {
			select {
			case <-sig:
				s.Stop()
				cancel := false
				survey.AskOne(
					&survey.Confirm{
						Message: "Do you want to cancel the deployment, this will lead to corrupted kubernetes environment?",
						Default: false,
					}, &cancel, nil)
				if cancel {
					s.Stop()
					StopAndRemoveContainer(s)
					os.Exit(1)
				} else {
					s.Start()
				}
			case <-done:
				return
			}
		}
	}()

	go func() {
		stdout, errStdout = copyAndCapture(os.Stdout, stdoutIn, s)
		wg.Done()
		done <- true
	}()

	stderr, errStderr = copyAndCapture(os.Stderr, stderrIn, s)
	wg.Wait()

	err = cmd.Wait()

	outStr, errStr := string(stdout), string(stderr)

	if errStdout != nil || errStderr != nil {
		Info("failed to capture stdout or stderr\n")
	}

	if !IsVerbose {
		s.Stop()
	}

	return outStr, errStr, nil
}

func StopAndRemoveContainer(s *spinner.Spinner) {
	Verbose("stopping the container")

	stopContainer := models.Command{
		Name: "docker",
		Args: []string{"stop", "xl-seed"},
	}
	ExecuteCommandAndShowLogs(stopContainer, s)

	Verbose("removing the container")
	rmContainer := models.Command{
		Name: "docker",
		Args: []string{"rm", "xl-seed"},
	}
	ExecuteCommandAndShowLogs(rmContainer, s)
}
