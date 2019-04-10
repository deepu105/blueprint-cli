package blueprint

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"text/template"

	"github.com/fatih/color"
	funk "github.com/thoas/go-funk"

	"github.com/magiconair/properties"
	"github.com/xebialabs/xl-cli/pkg/models"
	"github.com/xebialabs/xl-cli/pkg/util"

	"github.com/Masterminds/sprig"
	survey "gopkg.in/AlecAivazis/survey.v1"
)

// SkipFinalPrompt is used in tests to skip the confirmation prompt
var SkipFinalPrompt = false

const (
	valuesFile        = "values.xlvals"
	valuesFileHeader  = "# This file includes all non-secret values, you can add variables here and then refer them with '!value' tag in YAML files"
	secretsFile       = "secrets.xlvals"
	secretsFileHeader = "# This file includes all secret values, and will be excluded from GIT. You can add new values and/or edit them and then refer to them using '!value' YAML tag"
	gitignoreFile     = ".gitignore"
)

var ignoredPaths = []string{"__test__"}

func getFuncMaps() template.FuncMap {
	funcMaps := sprig.TxtFuncMap()
	funcMaps["kebabcase"] = util.ToKebabCase
	return funcMaps
}

func AdjustPathSeperatorIfNeeded(blueprintTemplate string) string {
	re := regexp.MustCompile(`[\/\\]`)
	return re.ReplaceAllString(blueprintTemplate, string(os.PathSeparator))
}

func shouldSkipFile(templateConfig TemplateConfig, variables *[]Variable, parameters map[string]interface{}) (bool, error) {
	if !util.IsStringEmpty(templateConfig.DependsOn.Val) {
		dependsOnVal, err := ParseDependsOnValue(templateConfig.DependsOn, variables, parameters)
		if err != nil {
			return false, err
		}
		return !dependsOnVal, nil
	}
	if !util.IsStringEmpty(templateConfig.DependsOnFalse.Val) {
		dependsOnFalseVal, err := ParseDependsOnValue(templateConfig.DependsOnFalse, variables, parameters)
		if err != nil {
			return false, err
		}
		return dependsOnFalseVal, nil
	}
	return false, nil
}

// InstantiateBlueprint is entry point for the cli command
func InstantiateBlueprint(
	blueprintLocalMode bool,
	templatePath string,
	blueprintContext *BlueprintContext,
	generatedBlueprint *GeneratedBlueprint,
	answersFile string,
	strictAnswers bool,
	useDefaultsAsValue bool,
	fromUpCommand bool,
	surveyOpts ...survey.AskOpt,
) error {
	var err error
	var blueprints map[string]*models.BlueprintRemote

	// if remote mode, initialize repository client
	if !blueprintLocalMode {
		util.Verbose("[cmd] Reading blueprints from provider: %s\n", (*blueprintContext.ActiveRepo).GetProvider())
		blueprints, err = blueprintContext.initCurrentRepoClient()
		if err != nil {
			return err
		}

		// if template path is not defined in cmd, get user selection
		if templatePath == "" {
			templatePath, err = blueprintContext.askUserToChooseBlueprint(blueprints, templatePath, surveyOpts...)
			if err != nil {
				return err
			}
		}
	} else {
		templatePath = AdjustPathSeperatorIfNeeded(templatePath)
	}

	// get local/remote blueprint definition
	blueprintDoc, err := getBlueprintConfig(blueprintContext, blueprintLocalMode, blueprints, templatePath)
	if err != nil {
		return err
	}

	// ask for user input
	preparedData, err := blueprintDoc.prepareTemplateData(answersFile, strictAnswers, useDefaultsAsValue, surveyOpts...)
	if err != nil {
		return err
	}
	util.Verbose("[dataPrep] Prepared data: %#v\n", preparedData)

	// if this is use-defaults mode, show used default values as table
	if useDefaultsAsValue && fromUpCommand {
		// Final prompt from user to start generation process
		toContinue := false
		question := models.UpFinalPrompt

		err := survey.AskOne(&survey.Confirm{Message: question, Default: true}, &toContinue, nil, surveyOpts...)
		if err != nil {
			return err
		}
		if !toContinue {
			util.Fatal("xl up command cancelled \n")
			return nil
		}
	}

	// save prepared data to values & secrets files
	err = writeConfigToFile(valuesFileHeader, preparedData.Values, generatedBlueprint, filepath.Join(generatedBlueprint.OutputDir, valuesFile))
	if err != nil {
		return err
	}
	err = writeConfigToFile(secretsFileHeader, preparedData.Secrets, generatedBlueprint, filepath.Join(generatedBlueprint.OutputDir, secretsFile))
	if err != nil {
		return err
	}

	// generate .gitignore file
	gitignoreData := secretsFile
	err = writeDataToFile(generatedBlueprint, filepath.Join(generatedBlueprint.OutputDir, gitignoreFile), &gitignoreData)
	if err != nil {
		return err
	}

	// execute each template file found
	for _, config := range blueprintDoc.TemplateConfigs {
		skipFile, err := shouldSkipFile(config, &blueprintDoc.Variables, preparedData.TemplateData)
		if err != nil {
			return err
		}

		if skipFile {
			util.Verbose("[file] skipping file [%s] since it has dependsOn value set\n", config.Path)
			continue
		}

		// read template contents
		util.Verbose("[file] Fetching template file %s from %s\n", config.Path, config.FullPath)
		templateContent, err := blueprintContext.fetchFileContents(config.FullPath, blueprintLocalMode, strings.HasSuffix(config.Path, templateExtension))
		if err != nil {
			return err
		}
		templateString := string(*templateContent)

		// process the template file (filter based on extension)
		if strings.HasSuffix(config.Path, templateExtension) {
			util.Verbose("[file] Processing template file %s\n", config.FullPath)

			// read & process the template
			tmpl := template.Must(template.New(config.Path).Funcs(getFuncMaps()).Parse(templateString))
			processedTmpl := &strings.Builder{}
			err = tmpl.Execute(processedTmpl, preparedData.TemplateData)
			if err != nil {
				return err
			}

			// write the processed template to a file
			finalTmpl := strings.TrimSpace(processedTmpl.String())
			err = writeDataToFile(generatedBlueprint, strings.Replace(config.Path, templateExtension, "", 1), &finalTmpl)
			if err != nil {
				return err
			}
		} else {
			if funk.ContainsString(ignoredPaths, filepath.Base(filepath.Dir(config.FullPath))) {
				// skip files under ignored directories
				util.Verbose("[file] Skipping file %s because path is under ignored list\n", config.FullPath)
			} else {
				// handle non-template files - copy as-it-is
				util.Verbose("[file] Copying file %s\n", config.FullPath)
				err = writeDataToFile(generatedBlueprint, config.Path, &templateString)
				if err != nil {
					return err
				}
			}
		}
	}
	util.Info("Please refer to file 'xebialabs/secrets.xlvals' for the default secrets\n")
	if blueprintDoc.Metadata.Instructions != "" {
		util.Info("\n\n%s\n\n", color.GreenString(blueprintDoc.Metadata.Instructions))
	}
	return nil
}

func getBlueprintConfig(blueprintContext *BlueprintContext, blueprintLocalMode bool, blueprints map[string]*models.BlueprintRemote, templatePath string) (*BlueprintConfig, error) {
	util.Verbose("[cmd] Parsing Blueprint from %s\n", templatePath)
	blueprintDoc, err := blueprintContext.parseDefinitionFile(blueprintLocalMode, blueprints, templatePath)
	if err != nil {
		return blueprintDoc, err
	}

	// if len(blueprintDoc.Include) > 0 {
	// 	util.Verbose("[dataPrep] Found %d included blueprints\n", len(blueprintDoc.Include))
	// 	err := composeBlueprints(blueprintDoc, blueprintContext, blueprintLocalMode, blueprints, templatePath)
	// 	if err != nil {
	// 		return blueprintDoc, err
	// 	}
	// }
	return blueprintDoc, nil
}

func composeBlueprints(blueprintDoc *BlueprintConfig, blueprintContext *BlueprintContext, blueprintLocalMode bool, blueprints map[string]*models.BlueprintRemote, templatePath string) error {
	for _, included := range blueprintDoc.Include {
		util.Verbose("[dataPrep] Fetch included blueprint %s\n", included.Blueprint)
		if included.Stage != "after" && included.Stage != "before" {
			included.Stage = "after"
		}
		// fetch blueprint from current repo
		composedBlueprintDoc, err := getBlueprintConfig(blueprintContext, blueprintLocalMode, blueprints, templatePath)
		if err != nil {
			return err
		}
		if composedBlueprintDoc != nil {
			// parse it
			// prepend/append params
			// prepend/append files
		}
	}
	return nil
}

func createDirectoryIfNeeded(fileName string) error {
	dir, _ := filepath.Split(fileName)
	if dir != "" && !util.PathExists(dir, true) {
		util.Verbose("[file] Creating sub-directory %s\n", dir)
		return os.MkdirAll(dir, os.ModePerm)
	}
	return nil
}

// --utility functions
func writeDataToFile(generatedBlueprint *GeneratedBlueprint, outputFileName string, data *string) error {
	util.Verbose("[file] Creating blueprint output file %s\n", outputFileName)
	file, err := generatedBlueprint.GetOutputFile(outputFileName)
	if err != nil {
		return err
	}
	out, err := file.WriteString(*data)
	if err != nil {
		return err
	}
	util.Verbose("\tWrote %d bytes \n", out)
	err = file.Sync()
	if err != nil {
		return err
	}
	err = file.Close()
	if err != nil {
		return err
	}
	util.Info("[file] Blueprint output file '%s' generated successfully\n", outputFileName)
	return nil
}

func writeConfigToFile(header string, config map[string]interface{}, generatedBlueprint *GeneratedBlueprint, filename string) error {
	props := properties.NewProperties()

	// sort based on keys
	var keys []string
	for k := range config {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		err := props.SetValue(k, config[k])
		if err != nil {
			return err
		}
	}

	// write properties to file
	f, err := generatedBlueprint.GetOutputFile(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	bytesWrittenHeader, err := f.Write([]byte(header + "\n"))
	if err != nil {
		return err
	}
	bytesWrittenConfig, err := props.Write(f, properties.UTF8)
	if err != nil {
		return err
	}
	util.Verbose("\tWrote %d bytes \n", bytesWrittenHeader+bytesWrittenConfig)
	util.Info("[file] Blueprint output file '%s' generated successfully\n", filename)
	return nil
}
