package main

import (
	"context"
	"flag"
	"fmt"
	ioutil "io/ioutil"

	proto "github.com/golang/protobuf/proto"
	google "golang.org/x/oauth2/google"
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
	tenantProject   = flag.String("tenant_project", "", "The tenant project.")
	zone            = flag.String("zone", "", "The instance zone.")
	region          = flag.String("region", "", "The enterprise instance region.")
	waitTime        = flag.Duration("wait_time",5*time.Minute,"Wait time for the cloud operation")
	sampleTime      = flag.Duration("sample_time",1*time.Minute,"sample time to write to SD")
	restartInterval =flag.Duration("restart_time",6*time.Hour,"Wait time for the cloud operation")
	dryrun, json    = flag.Bool("dry_run", true, "dry run the code"), flag.String("json", "", "json path")
    token = flag.Bool("token",false,"token based auth")
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

func readFile(name string) ([]byte,error) {
	out,err := ioutil.ReadFile(name)
	if err != nil {
		return out, err
	}
	return out,nil
}

func newVM(name string, zone string,exp int64, got int64) (*vm) {
	return &vm{
		name: name,
		zone: zone,
		expectState: exp,
		gotState: got,
}
}

var (
	status = map[string]int64{"RUNNING":10,"STOPPED":0,"STOPPING":1,"SUSPENDED":2,"SUSPENDING":3}
)

func updateVmStatus(ctx context.Context,c *compute.Service, tenantProject, zone, vmName string,dc chan *metrics.EventMetrics, expectOn int64){
	var got int64
	stat,err := vmmgr.CheckStatus(ctx,c,tenantProject, zone, vmName)
	if err != nil {
		return
	}
	st := stat.Status
	if _,ok := status[st]; !ok {
		got = 5
	} else {
		got = status[st]
	}
	vm := newVM(vmName, zone, expectOn,got)
	createMetrics(time.Now(), vm, dc)

}

func main(){
	ctx := context.Background()
	flag.Parse()
	var computeService *compute.Service
	var err error
	if *token {
		out, err := readFile(*json)
		if err != nil {
			log.Fatalf("Encountered error read file %v", err)
		}
		jwtConfig, err := google.JWTConfigFromJSON(out, compute.CloudPlatformScope)
		if err != nil {
			log.Fatalf("Unable to generate tokens from json file")
		}
		ts := jwtConfig.TokenSource(ctx)
		computeService, err = compute.NewService(ctx, option.WithScopes(compute.CloudPlatformScope), option.WithTokenSource(ts))
		if err != nil {
			fmt.Printf("Error while getting service, err: %v\n", err)
		}
	} else {
		cred, err := google.FindDefaultCredentials(ctx, compute.CloudPlatformScope)
		if err != nil {
			log.Fatalf("Unable to find default credentials")
		}
		ts := cred.TokenSource
		computeService, err = compute.NewService(ctx, option.WithScopes(compute.CloudPlatformScope), option.WithTokenSource(ts))
		if err != nil {
			fmt.Printf("Error while getting service, err: %v\n", err)
		}
	}

	var wg sync.WaitGroup
	// Print context logs to stdout.
	if *zone == "" && *region == "" {
		log.Fatalf("Please specify a valid zone or region\n")
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
	var startime = time.Now()
	log.Infof("restart interval is %v", *restartInterval)
	go func() {
		defer wg.Wait()
		for {
			if time.Since(startime) > *restartInterval {
				for _, v := range vms {
					log.Infof("Reboot initiating")
					if !*dryrun {
						if err = vmmgr.StopVMs(ctx, computeService, *tenantProject, *zone, v.Name, waitTime); err != nil {
							log.Fatalf(fmt.Sprintln(err))
						}
					} else {
						log.Infof("Dry run Stopping vm %v",v.Name)
					}
					updateVmStatus(ctx,computeService,*tenantProject,*zone,v.Name,dataChan,0)
					time.Sleep(1*time.Minute)
					log.Infof("Reboot completed")
			}
			for _, v := range vms{
				if err = vmmgr.StartVMs(ctx, computeService, *tenantProject, *zone, v.Name, waitTime); err != nil {
					log.Fatalf(fmt.Sprintln(err))
				}
				updateVmStatus(ctx,computeService,*tenantProject,*zone,v.Name,dataChan,10)
			}
			startime = time.Now()
			} else {
				for _,v := range vms {
					updateVmStatus(ctx,computeService,*tenantProject,*zone,v.Name,dataChan,10)
				}
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


