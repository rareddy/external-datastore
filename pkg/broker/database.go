package broker


type datasource struct {
	Name        string
	Parameters  map[string]interface{}
}

func newDataSource(serviceInstance DataSourceInstance, bindingParameters map[string]interface{}) datasource {
	ds := datasource{}
	ds.Name = "external-datastore"
	ds.Parameters = merge(serviceInstance.Parameters, bindingParameters)
	return ds
}

func merge(properties ...map[string]interface{}) map[string]interface{} {
	creds := make(map[string]interface{})

	for i := range properties {
		for k, v := range properties[i] {
			creds[k] = v
		}
	}
	return creds
}

func externalDatabaseName(ds datasource) string {
	return i2s(ds.Parameters["database-type"])
}

func externalDatabasePort(ds datasource) int {
	dbtype := i2s(ds.Parameters["database-type"])
	if dbtype == "mysql" {
		return 3306
	} else if dbtype == "sqlserver" {
		return 1433
	} else if dbtype == "oracle" {
		return 1521
	} else if dbtype == "postgresql" {
		return 5432
	} else if dbtype == "mongodb" {
		return 27017
	}
	panic("unknown data source")
}