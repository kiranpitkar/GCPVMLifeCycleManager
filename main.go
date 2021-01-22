package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/kiranpitkar/GCP-lifecycle-Manager/vmmgr"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	option "google.golang.org/api/option"
	"path"
	"time"

	log "github.com/sirupsen/logrus"

)
var (
	// Flags
	tenantProject = flag.String("tenant_project", "", "The tenant project.")
	zone          = flag.String("zone", "", "The instance zone.")
	region        = flag.String("region", "", "The enterprise instance region.")
	waitTime      = flag.Duration("wait_time",5*time.Minute,"Wait time for the cloud operation")
)

func main(){
	ctx := context.Background()
	flag.Parse()
	// Print context logs to stdout.
	if *zone == "" && *region == "" {
		log.Fatalf("Please specify a valid zone or region\n")
	}
	computeService, err := compute.NewService(ctx, option.WithScopes(compute.CloudPlatformScope))
	if err != nil {
		fmt.Printf("Error while getting service, err: %v\n", err)
	}
	clusterZones := []string{}
	if *zone != "" {
		clusterZones = append(clusterZones, *zone)
	} else {
		if clusterZones, err = listZones(ctx, computeService, *tenantProject, *region); err != nil {
			log.Fatalf(fmt.Sprintln(err))
		}
	}
	fmt.Printf("zones are %v", clusterZones)
	vms, err := vmmgr.ListVMs(ctx,computeService,*tenantProject,*zone)
	if err != nil {
		log.Fatalf(fmt.Sprintln(err))
	}
	fmt.Println(vms)
	for _, vm := range vms {
		if err = vmmgr.StopVMs(ctx, computeService, *tenantProject, *zone, vm.Name ); err != nil {
			log.Fatalf(fmt.Sprintln(err))
		}
	}
}


