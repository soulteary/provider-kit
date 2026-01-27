package provider

import "testing"

func TestSendResult_WithMetadata_NilMap(t *testing.T) {
	// Create result with nil metadata
	result := &SendResult{
		OK:       true,
		Provider: "test",
		Channel:  ChannelEmail,
		Metadata: nil,
	}

	// WithMetadata should initialize the map
	result.WithMetadata("key1", "value1")

	if result.Metadata == nil {
		t.Error("Metadata should be initialized")
	}
	if result.Metadata["key1"] != "value1" {
		t.Errorf("Metadata[key1] = %v, want value1", result.Metadata["key1"])
	}
}
