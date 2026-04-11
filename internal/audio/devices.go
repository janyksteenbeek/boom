package audio

// ListAudioDevices returns available audio output device names.
func ListAudioDevices() []string {
	return []string{"System Default"}
}
