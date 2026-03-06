package cloudflare

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestClientIntegrationCreateGetDelete 使用真实 Cloudflare API 做一次最小闭环联调。
// 该测试默认会被跳过，只有明确提供环境变量时才会执行。
func TestClientIntegrationCreateGetDelete(t *testing.T) {
	apiToken := os.Getenv("LINUXDOSPACE_CF_API_TOKEN")
	zoneID := os.Getenv("LINUXDOSPACE_CF_ZONE_ID")
	rootDomain := os.Getenv("LINUXDOSPACE_CF_ROOT_DOMAIN")
	if rootDomain == "" {
		rootDomain = "linuxdo.space"
	}

	if apiToken == "" || zoneID == "" {
		t.Skip("integration test skipped because LINUXDOSPACE_CF_API_TOKEN or LINUXDOSPACE_CF_ZONE_ID is missing")
	}

	client := NewClient(apiToken)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	recordName := "_linuxdospace-smoke-" + time.Now().UTC().Format("20060102150405") + "." + rootDomain
	recordContent := "smoke-" + time.Now().UTC().Format("150405")

	created, err := client.CreateDNSRecord(ctx, zoneID, CreateDNSRecordInput{
		Type:    "TXT",
		Name:    recordName,
		Content: recordContent,
		TTL:     120,
		Proxied: false,
		Comment: "linuxdospace integration test",
	})
	if err != nil {
		t.Fatalf("create dns record: %v", err)
	}

	defer func() {
		if err := client.DeleteDNSRecord(context.Background(), zoneID, created.ID); err != nil {
			t.Fatalf("cleanup dns record: %v", err)
		}
	}()

	fetched, err := client.GetDNSRecord(ctx, zoneID, created.ID)
	if err != nil {
		t.Fatalf("get dns record: %v", err)
	}

	if fetched.Name != recordName {
		t.Fatalf("expected record name %q, got %q", recordName, fetched.Name)
	}
	if fetched.Content != recordContent {
		t.Fatalf("expected record content %q, got %q", recordContent, fetched.Content)
	}
}
