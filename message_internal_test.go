package provider

import "testing"

func TestMessage_WithParam_NilParams(t *testing.T) {
	msg := &Message{
		To:     "test@example.com",
		Params: nil,
	}

	msg.WithParam("key1", "value1")

	if msg.Params == nil {
		t.Error("Params should be initialized")
	}
	if msg.Params["key1"] != "value1" {
		t.Errorf("Params[key1] = %v, want value1", msg.Params["key1"])
	}
}

func TestMessage_AddMetadata_NilMetadata(t *testing.T) {
	msg := &Message{
		To:       "test@example.com",
		Metadata: nil,
	}

	msg.AddMetadata("key1", "value1")

	if msg.Metadata == nil {
		t.Error("Metadata should be initialized")
	}
	if msg.Metadata["key1"] != "value1" {
		t.Errorf("Metadata[key1] = %v, want value1", msg.Metadata["key1"])
	}
}

func TestMessage_Clone_NilMaps(t *testing.T) {
	msg := &Message{
		To:       "test@example.com",
		Subject:  "Test",
		Body:     "Body",
		Params:   nil,
		Metadata: nil,
	}

	clone := msg.Clone()

	if clone.To != msg.To {
		t.Error("Clone() should copy To field")
	}
	// nil maps should remain nil in clone
	if clone.Params != nil {
		t.Error("Clone() should not create Params if original is nil")
	}
	if clone.Metadata != nil {
		t.Error("Clone() should not create Metadata if original is nil")
	}
}
