package broker

import (
	"net/http"
	"sync"

	"fmt"
	"github.com/golang/glog"
	osb "github.com/pmorie/go-open-service-broker-client/v2"
	"github.com/pmorie/osb-broker-lib/pkg/broker"
	"errors"
)

// NewDataSourceBroker is a hook that is called with the Options the program is run
// with. NewDataSourceBroker is the place where you will initialize your
// DataSourceBroker the parameters passed in.
func NewDataSourceBroker(o Options) (*DataSourceBroker, error) {
	// For example, if your DataSourceBroker requires a parameter from the command
	// line, you would unpack it from the Options and set it on the
	// DataSourceBroker here.
	return &DataSourceBroker{
		async:     o.Async,
		instances: make(map[string]*DataSourceInstance, 10),
	}, nil
}

// DataSourceBroker provides an implementation of the broker.DataSourceBroker
// interface.
type DataSourceBroker struct {
	// Indicates if the broker should handle the requests asynchronously.
	async bool
	// Synchronize go routines.
	sync.RWMutex
	// Add fields here! These fields are provided purely as an example
	instances map[string]*DataSourceInstance
}

type DataSourceInstance struct {
	ID                   string
	PlanID               string
	Parameters           map[string]interface{}
	inProgressOperations map[osb.OperationKey]osb.LastOperationState
	bindings             map[string]string
	namespace            string
	OperationKey         osb.OperationKey
	lastError            error
}

var _ broker.Interface = &DataSourceBroker{}

func (b *DataSourceBroker) GetCatalog(c *broker.RequestContext) (*osb.CatalogResponse, error) {
	response := &osb.CatalogResponse{}

	services, err := catalog()
	if err != nil {
		return nil, err
	}

	response.Services = services

	return response, nil
}

func (b *DataSourceBroker) Provision(request *osb.ProvisionRequest, c *broker.RequestContext) (*osb.ProvisionResponse, error) {

	b.Lock()
	defer b.Unlock()

	// only accept async
	if !request.AcceptsIncomplete {
		return nil, osb.HTTPStatusCodeError{
			StatusCode: http.StatusUnprocessableEntity,
		}
	}

	// if provision request on existing instance comes in then return 202
	if b.instances[request.InstanceID] != nil {
		glog.Infof("Already provisioned ", request.InstanceID)
		return nil, osb.HTTPStatusCodeError{
			StatusCode: http.StatusAccepted,
		}
	}

	glog.Infof("Received provision request for ", request.InstanceID)
	namespace := i2s(request.Parameters["namespace"])

	response := osb.ProvisionResponse{}
	operation := osb.OperationKey("provision")
	serviceInstance := DataSourceInstance{ID: request.InstanceID, Parameters: request.Parameters, PlanID: request.PlanID,
		OperationKey: operation, inProgressOperations: map[osb.OperationKey]osb.LastOperationState{},
		namespace: namespace, bindings:map[string]string{}}
	serviceInstance.inProgressOperations[operation] = osb.StateInProgress
	b.instances[request.InstanceID] = &serviceInstance

	response.Async = true

	// create external service in async mode
	after := func(opKey osb.OperationKey, err error) {
		if err != nil {
			fmt.Println("Failed to Provision service with Plan: " + request.PlanID + " Error: "+i2s(err))
			serviceInstance.inProgressOperations[opKey] = osb.StateFailed
			serviceInstance.lastError = err
		}
		serviceInstance.inProgressOperations[opKey] = osb.StateSucceeded
	}
	go serviceAction(serviceInstance, operation, createExternalService, after)

	url := "http://goolge.com"
	response.DashboardURL = &url
	// bug in OpenShift it is not returning this key back with lastoperation.
	response.OperationKey = &operation
	return &response, nil
}

func (b *DataSourceBroker) Deprovision(request *osb.DeprovisionRequest, c *broker.RequestContext) (*osb.DeprovisionResponse, error) {

	b.Lock()
	defer b.Unlock()

	fmt.Println("DeProvision Request started", i2s(request.InstanceID))
	response := osb.DeprovisionResponse{}
	operation := osb.OperationKey("deprovision")
	// if provision request on existing instance comes in then return 202
	instance := b.instances[request.InstanceID]
	if instance != nil  && instance.OperationKey == operation {
		glog.Infof("Already deprovisioned ", request.InstanceID)
		fmt.Println("Found Previos DeProvision Request ")
		return nil, osb.HTTPStatusCodeError{
			StatusCode: http.StatusAccepted,
		}
	}

	response.OperationKey = &operation
	response.Async = false

	serviceInstance, ok := b.instances[request.InstanceID]
	if !ok {
		return &response, nil
	}

	serviceInstance.inProgressOperations[operation] = osb.StateInProgress

	// removing the service
	after := func(opKey osb.OperationKey, err error) {
		if err != nil {
			fmt.Println("Failed to DeProvision service with Plan:" + request.PlanID + " Error: "+i2s(err))
			serviceInstance.inProgressOperations[opKey] = osb.StateFailed
			serviceInstance.lastError = err
		}
		serviceInstance.inProgressOperations[opKey] = osb.StateSucceeded
		delete(b.instances, request.InstanceID)
		fmt.Println("DeProvision service with Plan:" + request.PlanID)
	}
	serviceAction(*serviceInstance, operation, removeExternalService, after)

	response.Async = false
	return &response, nil
}

func (b *DataSourceBroker) LastOperation(request *osb.LastOperationRequest, c *broker.RequestContext) (*osb.LastOperationResponse, error) {
	serviceInstance := b.instances[request.InstanceID]
	glog.V(4).Infof("last operation request on instance", request)

	if serviceInstance == nil {
		return nil, errors.New("Service "+request.InstanceID+" not found")
	}

	// TODO:Bug in OpenShift OperationKey is always passed as nil.
	if request.OperationKey == nil && &serviceInstance.OperationKey != nil {
		request.OperationKey = &serviceInstance.OperationKey
	}

	if request.OperationKey != nil && *request.OperationKey == serviceInstance.OperationKey {
		response := osb.LastOperationResponse{}
		response.State = serviceInstance.inProgressOperations[*request.OperationKey]
		glog.Infof("LastOperation response", response)
		return &response, serviceInstance.lastError
	}

	return nil, nil
}

func (b *DataSourceBroker) Bind(request *osb.BindRequest, c *broker.RequestContext) (*osb.BindResponse, error) {
	b.Lock()
	defer b.Unlock()

	glog.V(4).Infof("Bind operation request on instance", request)

	serviceInstance, ok := b.instances[request.InstanceID]
	serviceInstance.lastError = nil
	if !ok {
		return nil, osb.HTTPStatusCodeError{
			StatusCode: http.StatusNotFound,
		}
	}
	operation := osb.OperationKey(request.BindingID)
	serviceInstance.OperationKey = operation
	credentials := createBindingParameters(*serviceInstance, request.Parameters)
	/*
		bindingAlias := i2s(request.Parameters["binding-alias"])
		namespace := i2s(serviceInstance.Parameters["namespace"])

		credentials["binding-alias"] = bindingAlias
		serviceInstance.inProgressOperations[operation] = osb.StateInProgress
		serviceInstance.bindings[request.BindingID] = bindingAlias

		// asynchronously create PodPreset and set the status once the async operation is done

		after := func(opKey osb.OperationKey, err error) {
			if err != nil {
				fmt.Println("failed to create the PodPreset for binding " + request.BindingID)
				serviceInstance.inProgressOperations[opKey] = osb.StateFailed
			}
			serviceInstance.inProgressOperations[opKey] = osb.StateSucceeded
		}
		go podPresetAction(bindingAlias, namespace, request.BindingID, operation, buildPodPreset, after)
		*/

	response := osb.BindResponse{
		Credentials: credentials,
	}

	serviceInstance.inProgressOperations[operation] = osb.StateSucceeded
	if request.AcceptsIncomplete {
		response.Async = b.async
	}
	response.OperationKey = &operation
	return &response, nil
}

func (b *DataSourceBroker) Unbind(request *osb.UnbindRequest, c *broker.RequestContext) (*osb.UnbindResponse, error) {
	glog.V(4).Infof("Bind operation request on instance", request)

	response := osb.UnbindResponse{}
	operation := osb.OperationKey(request.BindingID)
	serviceInstance, ok := b.instances[request.InstanceID]
	if !ok {
		fmt.Println("service instance not found to unbind")
		return &response, nil
	}

	serviceInstance.OperationKey = operation
	serviceInstance.inProgressOperations[operation] = osb.StateInProgress
	response.Async = request.AcceptsIncomplete
	serviceInstance.OperationKey = operation
	serviceInstance.lastError = nil

	/*
	// asynchronously remove PodPreset
	after := func(opKey osb.OperationKey, err error) {
		if err != nil {
			fmt.Println("Failed to create the PodPreset for binding " + request.BindingID)
			serviceInstance.inProgressOperations[opKey] = osb.StateFailed
		}
		serviceInstance.inProgressOperations[opKey] = osb.StateSucceeded
		delete(serviceInstance.bindings, request.BindingID)
	}
	go podPresetAction(serviceInstance.bindings[request.BindingID], serviceInstance.namespace, request.BindingID, operation, removePodPreset, after)
	*/
	serviceInstance.inProgressOperations[operation] = osb.StateSucceeded
	return &response, nil
}

func (b *DataSourceBroker) Update(request *osb.UpdateInstanceRequest, c *broker.RequestContext) (*osb.UpdateInstanceResponse, error) {
	// Your logic for updating a service goes here.
	response := osb.UpdateInstanceResponse{}
	if request.AcceptsIncomplete {
		response.Async = b.async
	}

	return &response, nil
}

func (b *DataSourceBroker) ValidateBrokerAPIVersion(version string) error {
	return nil
}
