package provider

import "testing"

func TestChannel_String(t *testing.T) {
	tests := []struct {
		channel Channel
		want    string
	}{
		{ChannelSMS, "sms"},
		{ChannelEmail, "email"},
		{ChannelHTTP, "http"},
		{Channel("custom"), "custom"},
	}

	for _, tt := range tests {
		t.Run(string(tt.channel), func(t *testing.T) {
			if got := tt.channel.String(); got != tt.want {
				t.Errorf("Channel.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChannel_IsValid(t *testing.T) {
	tests := []struct {
		channel Channel
		want    bool
	}{
		{ChannelSMS, true},
		{ChannelEmail, true},
		{ChannelHTTP, true},
		{Channel("custom"), false},
		{Channel(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.channel), func(t *testing.T) {
			if got := tt.channel.IsValid(); got != tt.want {
				t.Errorf("Channel.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSupportedChannels(t *testing.T) {
	channels := SupportedChannels()
	if len(channels) != 4 {
		t.Errorf("SupportedChannels() returned %d channels, want 4", len(channels))
	}

	expected := map[Channel]bool{
		ChannelSMS:      false,
		ChannelEmail:    false,
		ChannelHTTP:     false,
		ChannelDingTalk: false,
	}

	for _, ch := range channels {
		if _, ok := expected[ch]; !ok {
			t.Errorf("Unexpected channel: %v", ch)
		}
		expected[ch] = true
	}

	for ch, found := range expected {
		if !found {
			t.Errorf("Expected channel not found: %v", ch)
		}
	}
}
