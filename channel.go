package provider

// Channel represents the delivery channel type
type Channel string

const (
	// ChannelSMS represents SMS delivery channel
	ChannelSMS Channel = "sms"
	// ChannelEmail represents email delivery channel
	ChannelEmail Channel = "email"
	// ChannelHTTP represents generic HTTP API channel (for extensibility)
	ChannelHTTP Channel = "http"
)

// String returns the string representation of the channel
func (c Channel) String() string {
	return string(c)
}

// IsValid checks if the channel is a valid known channel type
func (c Channel) IsValid() bool {
	switch c {
	case ChannelSMS, ChannelEmail, ChannelHTTP:
		return true
	default:
		return false
	}
}

// SupportedChannels returns all supported channels
func SupportedChannels() []Channel {
	return []Channel{ChannelSMS, ChannelEmail, ChannelHTTP}
}
