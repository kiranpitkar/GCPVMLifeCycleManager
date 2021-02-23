package main

import (
	"context"
	"flag"
	"fmt"

	proto "github.com/golang/protobuf/proto"
	"github.com/google/cloudprober/metrics"
	"github.com/kiranpitkar/GCPVMLifeCycleManager/vmmgr"
	"github.com/google/cloudprober/surfacers"
	compute "google.golang.org/api/compute/v1"
	option "google.golang.org/api/option"
	surfacerpb "github.com/google/cloudprober/surfacers/proto"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)
var (
	// Flags
	tenantProject = flag.String("tenant_project", "", "The tenant project.")
	zone          = flag.String("zone", "", "The instance zone.")
	region        = flag.String("region", "", "The enterprise instance region.")
	waitTime      = flag.Duration("wait_time",5*time.Minute,"Wait time for the cloud operation")
	sampleTime = flag.Duration("sample_time",1*time.Minute,"sample time to write to SD")
)

type vm struct {
	name string
	expectState int64 // The state We expect a VM to be Start=1 Stop=0, We expect to be off only when we trigger
	gotState int64 // The state we receive when We do a get Start=1 Stop=0
	zone string
}

func createMetrics(ts time.Time, vm *vm, dc chan *metrics.EventMetrics ){
  em := metrics.NewEventMetrics(ts).
	  AddMetric("expectedstate", metrics.NewInt(vm.expectState)).
	  AddMetric("actualstate", metrics.NewInt(vm.gotState)).
	  AddLabel("vmname", vm.name).
	  AddLabel("zone",vm.zone)
    log.Infof(em.String())
	dc <- em
}

func newVM(name string, zone string,exp int64, got int64) (*vm) {
	return &vm{
		name: name,
		zone: zone,
		expectState: exp,
		gotState: got,
}
}
func main(){
	ctx := context.Background()
	flag.Parse()
	var wg sync.WaitGroup
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
		if clusterZones, err = vmmgr.ListZones(ctx, computeService, *tenantProject, *region); err != nil {
			log.Fatalf(fmt.Sprintln(err))
		}
	}
	fmt.Printf("zones are %v", clusterZones)
	vms, err := vmmgr.ListVMs(ctx,computeService,*tenantProject,*zone)
	if err != nil {
		log.Fatalf(fmt.Sprintln(err))
	}
	fmt.Println(vms)
	dataChan := make(chan *metrics.EventMetrics, 1000)
	sDef := &surfacerpb.SurfacerDef{
		Type: surfacerpb.Type_STACKDRIVER.Enum(),
		Name: proto.String("GLM_Surfacer"),
	}
	sd := []*surfacerpb.SurfacerDef{sDef}
	sfacers,err := surfacers.Init(ctx,sd)
	if err != nil {
		log.Fatalf("Unable to create surfacer %v",err)
	}
	go func() {
		defer wg.Wait()
		for {
			for _, v := range vms {
				vm := newVM(v.Name, *zone, 1, 1)
				createMetrics(time.Now(), vm, dataChan)
			}
			time.Sleep(*sampleTime)
		}
	}()
	for {
		em := <-dataChan
		for _, sd := range sfacers {
			sd.Write(ctx,em)
		}
		fmt.Println(em.String())
	}
	/*
	for _, vm := range vms {
		if err = vmmgr.StopVMs(ctx, computeService, *tenantProject, *zone, vm.Name, waitTime); err != nil {
			log.Fatalf(fmt.Sprintln(err))
		}
	}
	*/
}


