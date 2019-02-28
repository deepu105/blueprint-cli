package k8s

import (
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"testing"

	"github.com/xebialabs/xl-cli/pkg/models"
)

var simpleSampleKubeConfig = `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: 123==
    insecure-skip-tls-verify: true
    server: https://test.io:443
  name: testCluster
contexts:
- context:
    cluster: testCluster
    namespace: test
    user: testCluster_user
  name: testCluster
current-context: testCluster
kind: Config
preferences: {}
users:
- name: testCluster_user
  user:
    client-certificate-data: 123==
    client-key-data: 123==
    token: 6555565666666666666`

var sampleKubeConfig = `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: 123==
    server: https://test.hcp.eastus.azmk8s.io:443
  name: testCluster
- cluster:
    insecure-skip-tls-verify: true
    server: https://ocpm.test.com:8443
  name: ocpm-test-com:8443
- cluster:
    insecure-skip-tls-verify: true
    server: https://ocpm.test.com:8443
  name: testUserNotFound
contexts:
- context:
    cluster: ocpm-test-com:8443
    namespace: default
    user: test/ocpm-test-com:8443
  name: default/ocpm-test-com:8443/test
- context:
    cluster: testCluster
    namespace: test
    user: clusterUser_testCluster_testCluster
  name: testCluster
- context:
    cluster: testClusterNotFound
    namespace: test
    user: testClusterNotFound
  name: testClusterNotFound
- context:
    cluster: testUserNotFound
    namespace: test
    user: testUserNotFound
  name: testUserNotFound
current-context: testCluster
kind: Config
preferences: {}
users:
- name: clusterUser_testCluster_testCluster
  user:
    client-certificate-data: 123==
    client-key-data: 123==
    token: 6555565666666666666
- name: test/ocpm-test-com:8443
  user:
    client-certificate-data: 123==
- name: testClusterNotFound
  user:
    client-certificate-data: 123==`

func TestGetKubeConfigFile(t *testing.T) {
	defer os.RemoveAll("test")
	tests := []struct {
		name    string
		want    []byte
		wantErr bool
		prepare func()
	}{
		{
			"should error if file not found",
			nil,
			true,
			func() {
				os.Setenv("KUBECONFIG", "test")
			},
		},
		{
			"should read file from path set as KUBECONFIG",
			[]byte(sampleKubeConfig),
			false,
			func() {
				tmpDir := path.Join("test", "blueprints")
				os.MkdirAll(tmpDir, os.ModePerm)
				d1 := []byte(sampleKubeConfig)
				ioutil.WriteFile(path.Join(tmpDir, "config"), d1, os.ModePerm)
				os.Setenv("KUBECONFIG", path.Join(tmpDir, "config"))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.prepare()
			got, err := GetKubeConfigFile()
			if (err != nil) != tt.wantErr {
				t.Errorf("GetKubeConfigFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetKubeConfigFile() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseKubeConfig(t *testing.T) {
	type args struct {
		kubeConfigYaml []byte
	}
	tests := []struct {
		name           string
		kubeConfigYaml []byte
		want           K8sConfig
		wantErr        bool
	}{
		{
			"should error on paring invalid config yaml",
			[]byte("----- gggg test}"),
			K8sConfig{},
			true,
		},
		{
			"should parse a valid config yaml",
			[]byte(simpleSampleKubeConfig),
			K8sConfig{
				APIVersion:     "v1",
				CurrentContext: "testCluster",
				Clusters: []K8sCluster{
					{
						Name: "testCluster",
						Cluster: K8sClusterItem{
							Server:                   "https://test.io:443",
							CertificateAuthorityData: "123==",
							InsecureSkipTLSVerify:    true,
						},
					},
				},
				Contexts: []K8sContext{
					{
						Name: "testCluster",
						Context: K8sContextItem{
							Cluster:   "testCluster",
							Namespace: "test",
							User:      "testCluster_user",
						},
					},
				},
				Users: []K8sUser{
					{
						Name: "testCluster_user",
						User: K8sUserItem{
							ClientCertificateData: "123==",
							ClientKeyData:         "123==",
						},
					},
				},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseKubeConfig(tt.kubeConfigYaml)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseKubeConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseKubeConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetContext(t *testing.T) {
	config, _ := ParseKubeConfig([]byte(sampleKubeConfig))
	type args struct {
		config  K8sConfig
		context string
	}
	tests := []struct {
		name    string
		args    args
		want    K8SFnResult
		wantErr bool
	}{
		{
			"should error when context is not found",
			args{
				config:  config,
				context: "dummy",
			},
			K8SFnResult{},
			true,
		},
		{
			"should error when cluster is not found",
			args{
				config:  config,
				context: "testClusterNotFound",
			},
			K8SFnResult{},
			true,
		},
		{
			"should error when user is not found",
			args{
				config:  config,
				context: "testUserNotFound",
			},
			K8SFnResult{},
			true,
		},
		{
			"should find default context when context is not specified",
			args{
				config:  config,
				context: "",
			},
			K8SFnResult{
				cluster: K8sClusterItem{
					Server:                   "https://test.hcp.eastus.azmk8s.io:443",
					CertificateAuthorityData: "123==",
					InsecureSkipTLSVerify:    false,
				},
				context: K8sContextItem{
					Cluster:   "testCluster",
					Namespace: "test",
					User:      "clusterUser_testCluster_testCluster",
				},
				user: K8sUserItem{
					ClientCertificateData: "123==",
					ClientKeyData:         "123==",
				},
			},
			false,
		},
		{
			"should find specified context when context is specified",
			args{
				config:  config,
				context: "default/ocpm-test-com:8443/test",
			},
			K8SFnResult{
				cluster: K8sClusterItem{
					Server:                "https://ocpm.test.com:8443",
					InsecureSkipTLSVerify: true,
				},
				context: K8sContextItem{
					Cluster:   "ocpm-test-com:8443",
					Namespace: "default",
					User:      "test/ocpm-test-com:8443",
				},
				user: K8sUserItem{
					ClientCertificateData: "123==",
				},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetContext(tt.args.config, tt.args.context)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetContext() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetContext() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetK8SConfigFromSystem(t *testing.T) {
	defer os.RemoveAll("test")
	tmpDir := path.Join("test", "blueprints")
	os.MkdirAll(tmpDir, os.ModePerm)
	d1 := []byte(sampleKubeConfig)
	ioutil.WriteFile(path.Join(tmpDir, "config"), d1, os.ModePerm)
	os.Setenv("KUBECONFIG", path.Join(tmpDir, "config"))

	tests := []struct {
		name    string
		context string
		want    K8SFnResult
		wantErr bool
	}{
		{
			"should error when context is not found",
			"dummy",
			K8SFnResult{},
			true,
		},
		{
			"should error when cluster is not found",
			"testClusterNotFound",
			K8SFnResult{},
			true,
		},
		{
			"should error when user is not found",
			"testUserNotFound",
			K8SFnResult{},
			true,
		},
		{
			"should find default context when context is not specified",
			"",
			K8SFnResult{
				cluster: K8sClusterItem{
					Server:                   "https://test.hcp.eastus.azmk8s.io:443",
					CertificateAuthorityData: "123==",
					InsecureSkipTLSVerify:    false,
				},
				context: K8sContextItem{
					Cluster:   "testCluster",
					Namespace: "test",
					User:      "clusterUser_testCluster_testCluster",
				},
				user: K8sUserItem{
					ClientCertificateData: "123==",
					ClientKeyData:         "123==",
				},
			},
			false,
		},
		{
			"should find specified context when context is specified",
			"default/ocpm-test-com:8443/test",
			K8SFnResult{
				cluster: K8sClusterItem{
					Server:                "https://ocpm.test.com:8443",
					InsecureSkipTLSVerify: true,
				},
				context: K8sContextItem{
					Cluster:   "ocpm-test-com:8443",
					Namespace: "default",
					User:      "test/ocpm-test-com:8443",
				},
				user: K8sUserItem{
					ClientCertificateData: "123==",
				},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetK8SConfigFromSystem(tt.context)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetK8SConfigFromSystem() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetK8SConfigFromSystem() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCallK8SFuncByName(t *testing.T) {
	defer os.RemoveAll("test")
	tmpDir := path.Join("test", "blueprints")
	os.MkdirAll(tmpDir, os.ModePerm)
	d1 := []byte(sampleKubeConfig)
	ioutil.WriteFile(path.Join(tmpDir, "config"), d1, os.ModePerm)
	os.Setenv("KUBECONFIG", path.Join(tmpDir, "config"))

	type args struct {
		module string
		params []string
	}
	tests := []struct {
		name    string
		args    args
		want    models.FnResult
		wantErr bool
	}{
		{
			"should error when invalid module is specified",
			args{
				"k88s",
				[]string{""},
			},
			nil,
			true,
		},
		{
			"should return empty when invalid context is specified",
			args{
				"config",
				[]string{"test"},
			},
			&K8SFnResult{},
			false,
		},
		{
			"should fetch the k8s config with default context when valid module is specified",
			args{
				"cOnFiG", // to check case sensitivity
				[]string{""},
			},
			&K8SFnResult{
				cluster: K8sClusterItem{
					Server:                   "https://test.hcp.eastus.azmk8s.io:443",
					CertificateAuthorityData: "123==",
					InsecureSkipTLSVerify:    false,
				},
				context: K8sContextItem{
					Cluster:   "testCluster",
					Namespace: "test",
					User:      "clusterUser_testCluster_testCluster",
				},
				user: K8sUserItem{
					ClientCertificateData: "123==",
					ClientKeyData:         "123==",
				},
			},
			false,
		},
		{
			"should fetch the k8s config with given context context when valid module is specified",
			args{
				"cOnFiG", // to check case sensitivity
				[]string{"default/ocpm-test-com:8443/test"},
			},
			&K8SFnResult{
				cluster: K8sClusterItem{
					Server:                "https://ocpm.test.com:8443",
					InsecureSkipTLSVerify: true,
				},
				context: K8sContextItem{
					Cluster:   "ocpm-test-com:8443",
					Namespace: "default",
					User:      "test/ocpm-test-com:8443",
				},
				user: K8sUserItem{
					ClientCertificateData: "123==",
				},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CallK8SFuncByName(tt.args.module, tt.args.params...)
			if (err != nil) != tt.wantErr {
				t.Errorf("CallK8SFuncByName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CallK8SFuncByName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getK8SConfigField(t *testing.T) {
	config, _ := ParseKubeConfig([]byte(sampleKubeConfig))
	fnRes, _ := GetContext(config, "testCluster")
	type args struct {
		v     *K8SFnResult
		field string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			"should return <invalid Value> when fetching non existing field",
			args{
				&fnRes,
				"dummy",
			},
			"<invalid Value>",
		},
		{
			"should return value when fetching existing field",
			args{
				&fnRes,
				"cluster.Server",
			},
			"cluster",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getK8SConfigField(tt.args.v, tt.args.field); got != tt.want {
				t.Errorf("getK8SConfigField() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestK8SFnResult_GetResult(t *testing.T) {
	type fields struct {
		cluster K8sClusterItem
		context K8sContextItem
		user    K8sUserItem
	}
	type args struct {
		module string
		attr   string
		index  int
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    []string
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &K8SFnResult{
				cluster: tt.fields.cluster,
				context: tt.fields.context,
				user:    tt.fields.user,
			}
			got, err := result.GetResult(tt.args.module, tt.args.attr, tt.args.index)
			if (err != nil) != tt.wantErr {
				t.Errorf("K8SFnResult.GetResult() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("K8SFnResult.GetResult() = %v, want %v", got, tt.want)
			}
		})
	}
}
