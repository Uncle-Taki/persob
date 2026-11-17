package mongodb

import "testing"

func TestRetentionFilterKeepsUnprocessedMessages(t *testing.T) {
	filter := retentionFilter("telegram_messages")
	if _, ok := filter["expires_at"]; !ok {
		t.Fatal("retention filter must only target documents without an expiry")
	}
	status, ok := filter["processing_status"].(map[string]interface{})
	if !ok {
		t.Fatalf("processing status filter type = %T", filter["processing_status"])
	}
	if _, ok := status["$nin"]; !ok {
		t.Fatal("unprocessed status exclusion is missing")
	}
	dispatch, ok := filter["dispatch_pending"].(map[string]interface{})
	if !ok {
		t.Fatalf("dispatch filter type = %T", filter["dispatch_pending"])
	}
	if _, ok := dispatch["$ne"]; !ok {
		t.Fatal("dispatch-pending exclusion is missing")
	}
}

func TestRetentionFilterForDerivedRecordsOnlyChecksExpiry(t *testing.T) {
	filter := retentionFilter("extracted_messages")
	if len(filter) != 1 {
		t.Fatalf("derived retention filter = %#v", filter)
	}
}
