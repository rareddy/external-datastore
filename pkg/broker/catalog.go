package broker

import (
	"encoding/json"
	osb "github.com/pmorie/go-open-service-broker-client/v2"
	"io/ioutil"
)

func catalog() ([]osb.Service, error) {

	databaseBroker := osb.Service{}
	data, err := ioutil.ReadFile("/opt/servicebroker/service.json")
	if err == nil {

		err = json.Unmarshal([]byte(data), &databaseBroker)
		if err != nil {
			panic(err)
		}
	}
	return []osb.Service{databaseBroker}, nil
}