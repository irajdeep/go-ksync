package main

import (
	"database/sql"
	"flag"

	_ "github.com/go-sql-driver/mysql"
)

// ResourceQuota represent each resource quota object in memory
// TODO(irajdeep): replace this with client-go Resource Quota API type
type ResourceQuota struct {
	projectID int
	cpu       int
	memory    int
}

var (
	configmapName = flag.String("config-map-name", "namespace-controller", "pass the configmap name to be watched")
)

func main() {
	db, err := sql.Open("mysql", "resource_quota")
	if err != nil {
		panic(err.Error())
	}

	defer db.Close()

	// Fetch all the rowns from the table
	rows, err := db.Query("SELECT * FROM table")
	if err != nil {
		panic(err.Error())
	}

	resourceQuotas := make([]*ResourceQuota, 0)
	var p, c, m int

	for rows.Next() {
		if err := rows.Scan(&p, &c, &m); err != nil {
			panic(err.Error())
		}
		resourceQuotas = append(resourceQuotas, &ResourceQuota{p, c, m})
	}
	// TODO: Read configmap object and update it wih the soure of truth specifies in the database
}
