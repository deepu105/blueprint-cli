package aws

import (
	"fmt"

	"reflect"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/xebialabs/xl-cli/pkg/models"
)

const (
	Credentials = "credentials"
	Regions     = "regions"
)

type AWSFnResult struct {
	creds   credentials.Value
	regions []string
}

func (result *AWSFnResult) GetResult(module string, attr string, index int) ([]string, error) {
	switch module {
	case Credentials:
		if attr == "" {
			return nil, fmt.Errorf("requested credentials attribute is not set")
		}
		return []string{getAWSCredentialsField(&result.creds, attr)}, nil
	case Regions:
		if index != -1 {
			return result.regions[index : index+1], nil
		}
		return result.regions, nil
	default:
		return nil, fmt.Errorf("%s is not a valid AWS module", module)
	}
}

func getAWSCredentialsField(v *credentials.Value, field string) string {
	r := reflect.ValueOf(v)
	f := reflect.Indirect(r).FieldByName(field)
	return f.String()
}

// GetAvailableAWSRegions lists AWS regions for the service
func GetAvailableAWSRegionsForService(serviceName string) ([]string, error) {
	rs, exists := endpoints.RegionsForService(endpoints.DefaultPartitions(), endpoints.AwsPartitionID, serviceName)
	if !exists {
		return nil, fmt.Errorf("no valid AWS region found for AWS %s service", serviceName)
	}

	var regions []string
	for key := range rs {
		regions = append(regions, key)
	}

	return regions, nil
}

// GetAWSCredentialsFromSystem fetches stored AWS access keys from file or env keys
func GetAWSCredentialsFromSystem() (credentials.Value, error) {
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))
	return sess.Config.Credentials.Get()
}

// CallAWSFuncByName calls related AWS module function with parameters provided
func CallAWSFuncByName(module string, params ...string) (models.FnResult, error) {
	switch strings.ToLower(module) {
	case Credentials:
		creds, err := GetAWSCredentialsFromSystem()
		if err != nil {
			return nil, err
		}
		return &AWSFnResult{creds: creds}, nil
	case Regions:
		if len(params) < 1 || params[0] == "" {
			return nil, fmt.Errorf("service name parameter is required for AWS regions function")
		}
		regionsList, err := GetAvailableAWSRegionsForService(params[0])
		if err != nil {
			return nil, err
		}
		sort.Strings(regionsList)
		return &AWSFnResult{regions: regionsList}, err
	default:
		return nil, fmt.Errorf("%s is not a valid AWS module", module)
	}
}