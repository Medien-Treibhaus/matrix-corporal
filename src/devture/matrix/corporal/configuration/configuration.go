package configuration

import (
	"devture/matrix/corporal/matrix"
	"encoding/json"
	"fmt"
	"os"
)

type Configuration struct {
	Matrix         Matrix
	Reconciliation Reconciliation
	HttpApi        HttpApi
	HttpGateway    HttpGateway
	PolicyProvider PolicyProvider
	Misc           Misc
}

type HttpApi struct {
	Enabled                  bool
	ListenAddress            string
	AuthorizationBearerToken string
}

type HttpGateway struct {
	ListenAddress string
}

type Matrix struct {
	HomeserverDomainName     string
	HomeserverApiEndpoint    string
	AuthSharedSecret         string
	RegistrationSharedSecret string
	ReconciliatorUserId      string
	TimeoutMilliseconds      int
}

type Reconciliation struct {
	UserId                    string
	RetryIntervalMilliseconds int
}

type Misc struct {
	Debug bool
}

type PolicyProvider map[string]interface{}

func LoadConfiguration(filePath string) (*Configuration, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("Failed to read configuration from %s: %s", filePath, err)
	}

	decoder := json.NewDecoder(file)
	configuration := Configuration{}
	err = decoder.Decode(&configuration)
	if err != nil {
		return nil, fmt.Errorf("Failed to decode JSON: %s", err)
	}

	err = validateConfiguration(&configuration)
	if err != nil {
		return nil, fmt.Errorf("Failed to validate configuration: %s", err)
	}

	return &configuration, nil
}

func validateConfiguration(configuration *Configuration) error {
	if !matrix.IsFullUserIdOfDomain(configuration.Reconciliation.UserId, configuration.Matrix.HomeserverDomainName) {
		return fmt.Errorf(
			"Reconciliation user `%s` is not hosted on the managed homeserver domain (%s)",
			configuration.Reconciliation.UserId,
			configuration.Matrix.HomeserverDomainName,
		)
	}

	if configuration.Matrix.TimeoutMilliseconds <= 0 {
		return fmt.Errorf("Matrix.TimeoutMilliseconds needs to be a positive number")
	}

	if configuration.Reconciliation.RetryIntervalMilliseconds <= 0 {
		return fmt.Errorf("Reconciliation.RetryIntervalMilliseconds needs to be a positive number")
	}

	return nil
}