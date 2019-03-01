package k8s

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strconv"

	"github.com/mitchellh/go-homedir"
	"github.com/xebialabs/yaml"

	"reflect"
	"strings"

	"github.com/xebialabs/xl-cli/pkg/models"
	"github.com/xebialabs/xl-cli/pkg/util"
)

const (
	Config = "config"
)

type K8sConfig struct {
	APIVersion     string       `yaml:"apiVersion,omitempty"`
	Clusters       []K8sCluster `yaml:"clusters,omitempty"`
	Contexts       []K8sContext `yaml:"contexts,omitempty"`
	CurrentContext string       `yaml:"current-context,omitempty"`
	Users          []K8sUser    `yaml:"users,omitempty"`
}

type K8sCluster struct {
	Name    string         `yaml:"name,omitempty"`
	Cluster K8sClusterItem `yaml:"cluster,omitempty"`
}

type K8sClusterItem struct {
	Server                   string `yaml:"server,omitempty"`
	CertificateAuthorityData string `yaml:"certificate-authority-data,omitempty"`
	InsecureSkipTLSVerify    bool   `yaml:"insecure-skip-tls-verify,omitempty"`
}

type K8sContext struct {
	Name    string         `yaml:"name,omitempty"`
	Context K8sContextItem `yaml:"context,omitempty"`
}

type K8sContextItem struct {
	Cluster   string `yaml:"cluster,omitempty"`
	Namespace string `yaml:"namespace,omitempty"`
	User      string `yaml:"user,omitempty"`
}

type K8sUser struct {
	Name string      `yaml:"name,omitempty"`
	User K8sUserItem `yaml:"user,omitempty"`
}

type K8sUserItem struct {
	ClientCertificateData string `yaml:"client-certificate-data,omitempty"`
	ClientKeyData         string `yaml:"client-key-data,omitempty"`
}

type K8SFnResult struct {
	Cluster K8sClusterItem
	Context K8sContextItem
	User    K8sUserItem
}

func (result *K8SFnResult) GetResult(module string, attr string, index int) ([]string, error) {
	switch module {
	case Config:
		if attr == "" {
			return nil, fmt.Errorf("required attribute is not set")
		}

		// if requested, do exists check
		if attr == "IsAvailable" {
			return []string{strconv.FormatBool(result.Cluster.Server != "" && result.User.ClientCertificateData != "")}, nil
		}

		paths := strings.Split(attr, ".")
		if len(paths) < 2 {
			return nil, fmt.Errorf("field name pattern is invalid. It must follow 'cluster.server' notation, for example")
		}

		// return attribute
		return []string{getK8SConfigField(result, attr)}, nil
	default:
		return nil, fmt.Errorf("%s is not a valid Kubernetes module", module)
	}
}

func getK8SConfigField(res *K8SFnResult, attr string) string {
	flatFields := FlattenFields(*res)
	for k, field := range flatFields {
		if strings.ToLower(k) == strings.ToLower(attr) {
			return field.String()
		}
	}
	return ""
}

// CallK8SFuncByName calls related K8S module function with parameters provided
func CallK8SFuncByName(module string, params ...string) (models.FnResult, error) {
	switch strings.ToLower(module) {
	case Config:
		context := ""
		if len(params) > 0 && params[0] != "" {
			context = params[0]
		}
		config, err := GetK8SConfigFromSystem(context)
		if err != nil {
			util.Verbose("[aws] Error while processing function [%s] is: %v\n", module, err)
			// handle K8S configuration errors gracefully
			return &K8SFnResult{}, nil
		}
		return &config, nil
	default:
		return nil, fmt.Errorf("%s is not a valid K8S module", module)
	}
}

// Utilities

// GetK8SCredentialsFromSystem fetches stored K8S access keys from file or env keys
func GetK8SConfigFromSystem(context string) (K8SFnResult, error) {
	// fetch k8s config yaml and parse
	kubeConfigYaml, err := GetKubeConfigFile()
	if err != nil {
		return K8SFnResult{}, err
	}
	result, err := ParseKubeConfig(kubeConfigYaml)
	if err != nil {
		return K8SFnResult{}, err
	}
	if len(result.Contexts) == 0 || len(result.Clusters) == 0 {
		return K8SFnResult{}, fmt.Errorf("Kubernetes configuration file does not have any context/cluster defined")
	}
	// get requested context
	contextRes, err := GetContext(result, context)
	if err != nil {
		return K8SFnResult{}, err
	}
	return contextRes, nil
}

func GetKubeConfigFile() ([]byte, error) {
	// check if KUBECONFIG is set in environment
	configPath := os.Getenv("KUBECONFIG")
	if configPath == "" {
		// if KUBECONFIG is not set find path based on OS
		home, err := homedir.Dir()
		if err != nil {
			return nil, err
		}
		configPath = path.Join(home, ".kube", "config")
	}
	// read file from path and return string
	return ioutil.ReadFile(configPath)
}

func ParseKubeConfig(kubeConfigYaml []byte) (K8sConfig, error) {
	// parse yaml
	res := K8sConfig{}
	err := yaml.Unmarshal(kubeConfigYaml, &res)
	if err != nil {
		return res, err
	}
	return res, nil
}

func GetContext(config K8sConfig, context string) (K8SFnResult, error) {
	if context == "" {
		context = config.CurrentContext
	}
	var contextItem K8sContextItem
	for _, c := range config.Contexts {
		if strings.ToLower(c.Name) == strings.ToLower(context) {
			contextItem = c.Context
		}
	}
	if contextItem == (K8sContextItem{}) {
		return K8SFnResult{}, fmt.Errorf("Specified context was not found in the Kubernetes config file")
	}
	var clusterItem K8sClusterItem
	for _, c := range config.Clusters {
		if c.Name == contextItem.Cluster {
			clusterItem = c.Cluster
		}
	}
	if clusterItem == (K8sClusterItem{}) {
		return K8SFnResult{}, fmt.Errorf("No cluster found for specified context in the Kubernetes config file")
	}
	var userItem K8sUserItem
	for _, c := range config.Users {
		if c.Name == contextItem.User {
			userItem = c.User
		}
	}
	if userItem == (K8sUserItem{}) {
		return K8SFnResult{}, fmt.Errorf("No user found for specified context in the Kubernetes config file")
	}
	result := K8SFnResult{
		Cluster: clusterItem,
		Context: contextItem,
		User:    userItem,
	}
	return result, nil
}

func FlattenFields(iface interface{}) map[string]reflect.Value {
	fields := make(map[string]reflect.Value, 0)
	ifv := reflect.ValueOf(iface)
	ift := reflect.TypeOf(iface)

	for i := 0; i < ift.NumField(); i++ {
		v := ifv.Field(i)
		t := ift.Field(i)

		switch v.Kind() {
		case reflect.Struct:
			for k, v := range FlattenFields(v.Interface()) {
				fields[t.Name+"."+k] = v
			}
		default:
			fields[t.Name] = v
		}
	}
	return fields
}
