package vmmgr

import (
	"context"
	"fmt"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"path"
	"time"

	log "github.com/sirupsen/logrus"
)

func ListZones(ctx context.Context, gceSvc *compute.Service, tenantProject, region string) ([]string, error) {
	zoneFilter := fmt.Sprintf("name = %s-*", region)
	// Fetch all zones in the target region.
	zonesListCall := gceSvc.Zones.List(tenantProject).Filter(zoneFilter)
	pageToken := ""
	var zoneList []*compute.Zone
	for {
		resp, err := zonesListCall.PageToken(pageToken).Do()
		if err != nil {
			return nil, fmt.Errorf("Encountered error when listing zones: %v", err)
		}
		zoneList = append(zoneList, resp.Items...)
		if pageToken = resp.NextPageToken; pageToken == "" {
			break
		}
	}
	zoneNameList := []string{}
	for _, zone := range zoneList {
		zoneNameList = append(zoneNameList, zone.Name)
	}
	return zoneNameList, nil
}

func ListVMs(ctx context.Context, gceSvc *compute.Service, tenantProject, zone string) ([]*compute.Instance, error) {
	instancesListCall := gceSvc.Instances.List(tenantProject, zone)
	pageToken := ""
	var vmList []*compute.Instance
	for {
		resp, err := instancesListCall.PageToken(pageToken).Do()
		fmt.Println(resp)
		if err != nil {
			return nil, fmt.Errorf("Encountered error when listing instances: %v", err)
		}
		fmt.Printf("respone is %v \n", resp.Id)
		for _, i := range resp.Items {
			fmt.Printf("resp are %v", i.Name)
		}
		vmList = append(vmList, resp.Items...)
		if pageToken = resp.NextPageToken; pageToken == "" {
			break
		}
	}
	fmt.Println(vmList)
	return vmList, nil
}

func StopVMs(ctx context.Context, gceSvc *compute.Service, tenantProject, zone, vmName string, wait *time.Duration) error {
	op, err := gceSvc.Instances.Stop(tenantProject, zone, vmName).Context(ctx).Do()
	if err != nil {
		return err
	}
	_, err = waitOperation(ctx, gceSvc, tenantProject, op, wait)
	return err
}

func StartVMs(ctx context.Context, gceSvc *compute.Service, tenantProject, zone, vmName string, wait *time.Duration) error {
	op, err := gceSvc.Instances.Start(tenantProject, zone, vmName).Context(ctx).Do()
	if err != nil {
		return err
	}
	_, err = waitOperation(ctx, gceSvc, tenantProject, op, wait)
	return err
}

func waitOperation(ctx context.Context, gceSvc *compute.Service, tenantProject string, op *compute.Operation, wait *time.Duration) (uint64, error) {
	name, zone, region := op.Name, op.Zone, op.Region
	ctx, cancel := context.WithTimeout(ctx, *wait)
	var err error
	defer cancel()
	for {
		switch {
		case zone != "":
			op, err = gceSvc.ZoneOperations.Get(tenantProject, path.Base(zone), name).Context(ctx).Do()
		case region != "":
			op, err = gceSvc.RegionOperations.Get(tenantProject, path.Base(region), name).Context(ctx).Do()
		default:
			op, err = gceSvc.GlobalOperations.Get(tenantProject, name).Context(ctx).Do()
		}
		if err != nil {
			// Retry 503 Service Unavailable.
			if apiErr, ok := err.(*googleapi.Error); !ok || apiErr.Code != 503 {
				return 0, err
			}
			log.Warn("transient error polling operation status for %s (will retry): %v", name, err)
		} else {
			if op != nil && op.Status == "DONE" {
				if op.Error != nil && len(op.Error.Errors) > 0 {
					return 0, fmt.Errorf("operation completes with error %v", op.Error.Errors[0])
				}
				return op.TargetId, nil
			}
		}

		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("context is done while waiting for operation %s", name)
		case <-time.After(5 * time.Second):
			// Continue the loop.
		}
	}
}

func CheckStatus(ctx context.Context, gceSvc *compute.Service, tenantProject, zone, vmName string)(*compute.Instance, error ){
	resp, err := gceSvc.Instances.Get(tenantProject,zone,vmName).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	return resp, nil
}