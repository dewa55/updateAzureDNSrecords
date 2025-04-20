package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

func fetchPublicIp() (string, error) {
	public_ip_url := "https://api.ipify.org?format=text"
	response, err := http.Get(public_ip_url)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	ip, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	return string(ip), nil
}

func getRequiredEnv(name string) string {
	value := os.Getenv(name)
	if len(value) == 0 {
		log.Fatalf("Environment variable %s is missing", name)
	}
	return value
}

func createOrUpdateDNSRecord(
	ctx context.Context,
	client *armdns.RecordSetsClient,
	resourceGroup string,
	zoneName string,
	recordName string,
	ipAddress string,
) (*armdns.RecordSet, error) {
	resp, err := client.CreateOrUpdate(
		ctx,
		resourceGroup,
		zoneName,
		recordName,
		armdns.RecordTypeA,
		armdns.RecordSet{
			Properties: &armdns.RecordSetProperties{
				ARecords: []*armdns.ARecord{
					{IPv4Address: &ipAddress},
				},
				TTL: to(3600),
			},
		},
		nil,
	)
	if err != nil {
		return nil, err
	}
	return &resp.RecordSet, nil
}

func cleanup(
	ctx context.Context,
	client *armresources.ResourceGroupsClient,
	resourceGroup string,
) error {
	pollerResp, err := client.BeginDelete(ctx, resourceGroup, nil)
	if err != nil {
		return err
	}

	_, err = pollerResp.PollUntilDone(ctx, nil)
	if err != nil {
		return err
	}
	return nil
}

// Helper function to convert int32 to pointer
func to(i int32) *int64 {
	converted := int64(i)
	return &converted
}

func main() {
	// Get configuration from environment variables
	subscriptionID := getRequiredEnv("AZURE_SUBSCRIPTION_ID")
	resourceGroupName := getRequiredEnv("AZURE_RESOURCE_GROUP")
	zoneName := getRequiredEnv("AZURE_DNS_ZONE_NAME")
	recordNamesStr := getRequiredEnv("AZURE_RELATIVE_RECORD_SET_NAME")

	// Split comma-separated record names
	recordNames := strings.Split(recordNamesStr, ",")

	// Get credentials and create context
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		log.Fatal("Failed to get Azure credentials:", err)
	}
	ctx := context.Background()

	// Create client factories
	resourcesClientFactory, err := armresources.NewClientFactory(subscriptionID, cred, nil)
	if err != nil {
		log.Fatal("Failed to create resources client factory:", err)
	}

	dnsClientFactory, err := armdns.NewClientFactory(subscriptionID, cred, nil)
	if err != nil {
		log.Fatal("Failed to create DNS client factory:", err)
	}

	// Create specific clients
	recordSetsClient := dnsClientFactory.NewRecordSetsClient()
	resourceGroupClient := resourcesClientFactory.NewResourceGroupsClient()

	// Fetch public IP once
	ip, err := fetchPublicIp()
	if err != nil {
		log.Fatal("Failed to fetch public IP:", err)
	}
	log.Println("Detected public IP:", ip)

	// Create or update multiple DNS records
	for _, recordName := range recordNames {
		recordName = strings.TrimSpace(recordName)
		log.Printf("Updating DNS record %s...", recordName)

		recordSet, err := createOrUpdateDNSRecord(
			ctx,
			recordSetsClient,
			resourceGroupName,
			zoneName,
			recordName,
			ip,
		)
		if err != nil {
			log.Printf("Failed to update DNS record %s: %v", recordName, err)
			continue
		}
		log.Printf("DNS record %s updated successfully: %s", recordName, *recordSet.ID)
	}

	// In most cases, you don't want to delete the DNS zone, so likely keep this value set
	keepResource := os.Getenv("KEEP_RESOURCE")
	if keepResource != "true" {
		log.Println("Cleaning up resources...")
		err = cleanup(ctx, resourceGroupClient, resourceGroupName)
		if err != nil {
			log.Fatal("Failed to clean up resources:", err)
		}
		log.Println("Resources cleaned up successfully.")
	}
}
