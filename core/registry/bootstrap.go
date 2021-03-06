package registry

import (
	"errors"

	"github.com/go-chassis/go-chassis/core/common"
	"github.com/go-chassis/go-chassis/core/config"
	"github.com/go-chassis/go-chassis/core/config/schema"
	"github.com/go-chassis/go-chassis/core/lager"
	"github.com/go-chassis/go-chassis/core/metadata"
	"github.com/go-chassis/go-chassis/pkg/runtime"
)

var errEmptyServiceIDFromRegistry = errors.New("got empty serviceID from registry")

// microServiceDependencies micro-service dependencies
var microServiceDependencies *MicroServiceDependency

// InstanceEndpoints instance endpoints
var InstanceEndpoints map[string]string

// RegisterMicroservice register micro-service
func RegisterMicroservice() error {
	service := config.MicroserviceDefinition
	if e := service.ServiceDescription.Environment; e != "" {
		lager.Logger.Infof("Microservice environment: [%s]", e)
	} else {
		lager.Logger.Debug("No microservice environment defined")
	}
	microServiceDependencies = &MicroServiceDependency{}
	schemas, err := schema.GetSchemaIDs(service.ServiceDescription.Name)
	if err != nil {
		lager.Logger.Warnf("No schemas file for microservice [%s].", service.ServiceDescription.Name)
		schemas = make([]string, 0)
	}
	if service.ServiceDescription.Level == "" {
		service.ServiceDescription.Level = common.DefaultLevel
	}
	if service.ServiceDescription.Properties == nil {
		service.ServiceDescription.Properties = make(map[string]string)
	}
	framework := metadata.NewFramework()

	svcPaths := service.ServiceDescription.ServicePaths
	var regpaths []ServicePath
	for _, svcPath := range svcPaths {
		var regpath ServicePath
		regpath.Path = svcPath.Path
		regpath.Property = svcPath.Property
		regpaths = append(regpaths, regpath)
	}
	microservice := &MicroService{
		ServiceID:   runtime.ServiceID,
		AppID:       runtime.App,
		ServiceName: service.ServiceDescription.Name,
		Version:     service.ServiceDescription.Version,
		Paths:       regpaths,
		Environment: service.ServiceDescription.Environment,
		Status:      common.DefaultStatus,
		Level:       service.ServiceDescription.Level,
		Schemas:     schemas,
		Framework: &Framework{
			Version: framework.Version,
			Name:    framework.Name,
		},
		RegisterBy: framework.Register,
		Metadata:   make(map[string]string),
		// TODO allows to customize microservice alias
		Alias: "",
	}
	//update metadata
	if len(microservice.Alias) == 0 {
		// if the microservice is allowed to be called by consumers with different appId,
		// this means that the governance configuration of the consumer side needs to
		// support key format with appid, like 'cse.loadbalance.{alias}.strategy.name'.
		microservice.Alias = microservice.AppID + ":" + microservice.ServiceName
	}
	if config.GetRegistratorScope() == common.ScopeFull {
		microservice.Metadata["allowCrossApp"] = common.TRUE
		service.ServiceDescription.Properties["allowCrossApp"] = common.TRUE
	} else {
		service.ServiceDescription.Properties["allowCrossApp"] = common.FALSE
	}
	lager.Logger.Debugf("Update micro service properties%v", service.ServiceDescription.Properties)
	lager.Logger.Infof("Framework registered is [ %s:%s ]", framework.Name, framework.Version)
	lager.Logger.Infof("Micro service registered by [ %s ]", framework.Register)

	sid, err := DefaultRegistrator.RegisterService(microservice)
	if err != nil {
		lager.Logger.Errorf("Register [%s] failed: %s", microservice.ServiceName, err)
		return err
	}
	if sid == "" {
		lager.Logger.Error(errEmptyServiceIDFromRegistry.Error())
		return errEmptyServiceIDFromRegistry
	}
	runtime.ServiceID = sid
	lager.Logger.Infof("Register [%s/%s] success", runtime.ServiceID, microservice.ServiceName)

	for _, schemaID := range schemas {
		schemaInfo := schema.DefaultSchemaIDsMap[schemaID]
		DefaultRegistrator.AddSchemas(sid, schemaID, schemaInfo)
	}

	return nil
}

// RegisterMicroserviceInstances register micro-service instances
func RegisterMicroserviceInstances() error {
	lager.Logger.Info("Start to register instance.")
	service := config.MicroserviceDefinition
	var err error

	sid, err := DefaultServiceDiscoveryService.GetMicroServiceID(runtime.App, service.ServiceDescription.Name, service.ServiceDescription.Version, service.ServiceDescription.Environment)
	if err != nil {
		lager.Logger.Errorf("Get service failed, key: %s:%s:%s, err %s",
			runtime.App,
			service.ServiceDescription.Name,
			service.ServiceDescription.Version, err)
		return err
	}
	eps, err := MakeEndpointMap(config.GlobalDefinition.Cse.Protocols)
	if err != nil {
		return err
	}
	lager.Logger.Infof("service support protocols %s", config.GlobalDefinition.Cse.Protocols)
	if InstanceEndpoints != nil {
		eps = InstanceEndpoints
	}

	microServiceInstance := &MicroServiceInstance{
		EndpointsMap: eps,
		HostName:     runtime.HostName,
		Status:       common.DefaultStatus,
		Metadata:     map[string]string{"nodeIP": config.NodeIP},
	}

	var dInfo = new(DataCenterInfo)
	if config.GlobalDefinition.DataCenter.Name != "" && config.GlobalDefinition.DataCenter.AvailableZone != "" {
		dInfo.Name = config.GlobalDefinition.DataCenter.Name
		dInfo.Region = config.GlobalDefinition.DataCenter.Name
		dInfo.AvailableZone = config.GlobalDefinition.DataCenter.AvailableZone
		microServiceInstance.DataCenterInfo = dInfo
	}

	instanceID, err := DefaultRegistrator.RegisterServiceInstance(sid, microServiceInstance)
	if err != nil {
		lager.Logger.Errorf("Register instance failed, serviceID: %s, err %s", err)
		return err
	}
	//Set to runtime
	runtime.InstanceID = instanceID
	runtime.InstanceStatus = runtime.StatusRunning
	if service.ServiceDescription.InstanceProperties != nil {
		if err := DefaultRegistrator.UpdateMicroServiceInstanceProperties(sid, instanceID, service.ServiceDescription.InstanceProperties); err != nil {
			lager.Logger.Errorf("UpdateMicroServiceInstanceProperties failed, microServiceID/instanceID = %s/%s.", sid, instanceID)
			return err
		}
		lager.Logger.Debugf("UpdateMicroServiceInstanceProperties success, microServiceID/instanceID = %s/%s.", sid, instanceID)
	}

	value, _ := SelfInstancesCache.Get(microServiceInstance.ServiceID)
	instanceIDs, _ := value.([]string)
	var isRepeat bool
	for _, va := range instanceIDs {
		if va == instanceID {
			isRepeat = true
		}
	}
	if !isRepeat {
		instanceIDs = append(instanceIDs, instanceID)
	}
	SelfInstancesCache.Set(sid, instanceIDs, 0)
	lager.Logger.Infof("Register instance success, serviceID/instanceID: %s/%s.", sid, instanceID)
	return nil
}
