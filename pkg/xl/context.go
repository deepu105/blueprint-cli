package xl

import (
	"fmt"
	"github.com/xebialabs/xl-cli/pkg/models"
	"net/url"

	"github.com/xebialabs/xl-cli/pkg/blueprint"
	"github.com/xebialabs/xl-cli/pkg/util"
    "time"
)

type ChangedIds struct {
	Kind    string
	Created *[]string
	Updated *[]string
}

type CiValidationError struct {
	CiId         string
	PropertyName string
	Message      string
}

type PermissionError struct {
	CiId       string
	Permission string
}

type DocumentFieldError struct {
	Field   string
	Problem string
}

type Errors struct {
	Validation *[]CiValidationError
	Permission *[]PermissionError
	Document   *DocumentFieldError
	Generic    *string
}

type TaskInfo struct {
	Id          string
	Description string
}

type Changes struct {
	Ids  *[]ChangedIds
	Task *TaskInfo
}

type AsCodeResponse struct {
	Changes *Changes
	Errors  *Errors
	RawBody string
}

type Context struct {
	XLDeploy         XLServer
	XLRelease        XLServer
	BlueprintContext *blueprint.BlueprintContext
	values           map[string]string
	vcsInfo          *VCSInfo
}

type CurrentStep struct {
	Name      string
	State     string
	Automated bool
}

type TaskState struct {
	State        string
	CurrentSteps []CurrentStep
}

type VCSInfo struct {
    filename string
    vcsType  string
    remote   string
    commit   string
    author   string
    date     time.Time
    message  string
}

func (c *Context) PrintConfiguration() {
	util.Info("XL Deploy:\n  URL: %s\n  Username: %s\n  Applications home: %s\n  Environments home: %s\n  Infrastructure home: %s\n  Configuration home: %s\n",
		c.XLDeploy.(*XLDeployServer).Server.(*SimpleHTTPServer).Url.String(),
		c.XLDeploy.(*XLDeployServer).Server.(*SimpleHTTPServer).Username,
		c.XLDeploy.(*XLDeployServer).ApplicationsHome,
		c.XLDeploy.(*XLDeployServer).EnvironmentsHome,
		c.XLDeploy.(*XLDeployServer).InfrastructureHome,
		c.XLDeploy.(*XLDeployServer).ConfigurationHome)

	util.Info("XL Release:\n  URL: %s\n  Username: %s\n  Home: %s\n",
		c.XLRelease.(*XLReleaseServer).Server.(*SimpleHTTPServer).Url.String(),
		c.XLRelease.(*XLReleaseServer).Server.(*SimpleHTTPServer).Username,
		c.XLRelease.(*XLReleaseServer).Home)

	util.Info("Active Blueprint Context:\n  %s\n", (*c.BlueprintContext.ActiveRepo).GetInfo())
}

func (c *Context) GetDocumentHandlingServer(doc *Document) (XLServer, error) {
	if c.XLDeploy != nil && c.XLDeploy.AcceptsDoc(doc) {
		return c.XLDeploy, nil
	}

	if c.XLRelease != nil && c.XLRelease.AcceptsDoc(doc) {
		return c.XLRelease, nil
	}

	return nil, fmt.Errorf("unknown apiVersion: %s", doc.ApiVersion)
}

func (c *Context) preProcessAndGetServer(doc *Document, artifactsDir string) (XLServer, error) {
	err := doc.Preprocess(c, artifactsDir)
	if err != nil {
		return nil, err
	}

	if doc.ApiVersion == "" {
		return nil, fmt.Errorf("apiVersion missing")
	}
	server, err := c.GetDocumentHandlingServer(doc)
	if err != nil {
		return nil, err
	}
	return server, nil
}

func (c *Context) ProcessSingleDocument(doc *Document, artifactsDir string) (*Changes, error) {
	server, err := c.preProcessAndGetServer(doc, artifactsDir)
	defer doc.Cleanup()
	if err != nil {
		return nil, err
	}
	return server.SendDoc(doc, c.vcsInfo)
}

func (c *Context) ProcessSingleDocumentAndGetContents(doc *Document, artifactsDir string, fileName string) ([]byte, error) {
	err := doc.ConditionalPreprocess(c, artifactsDir)
	if err != nil {
		return nil, err
	}

	defer doc.Cleanup()

	documentBytes, err := doc.RenderYamlDocument()

	if doc.ApiVersion == "" {
		return nil, fmt.Errorf("apiVersion missing")
	}

	return documentBytes, err
}

func (c *Context) PreviewSingleDocument(doc *Document, artifactsDir string) (*models.PreviewResponse, error) {
	server, err := c.preProcessAndGetServer(doc, artifactsDir)
	defer doc.Cleanup()
	if err != nil {
		return nil, err
	}
	return server.PreviewDoc(doc)
}

func (c *Context) GenerateSingleDocument(generateServer string, generateFilename string, generatePath string, generateOverride bool, generatePermissions bool, users bool, roles bool, environments bool, applications bool, includePasswords bool) error {
	finalPath := url.QueryEscape(generatePath)

	if generateServer == string(models.XLD) {
		if generatePath != "" {
			util.Info("Generating definitions for path %s from XL Deploy to %s\n", generatePath, generateFilename)
		} else {
			util.Info("Generating definitions from XL Deploy to %s\n", generateFilename)
		}
		return c.XLDeploy.GenerateDoc(generateFilename, finalPath, generateOverride, generatePermissions, users, roles, false, false, includePasswords)
	}

	if generateServer == string(models.XLR) {
		if generatePath != "" {
			util.Info("Generating definitions for path %s from XL Release to %s\n", generatePath, generateFilename)
		} else {
			util.Info("Generating definitions from XL Release to %s\n", generateFilename)
		}
		return c.XLRelease.GenerateDoc(generateFilename, finalPath, generateOverride, generatePermissions, users, roles, environments, applications, includePasswords)
	}

	return fmt.Errorf("unknown server type: %s", generateServer)
}
