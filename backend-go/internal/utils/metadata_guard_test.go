package utils

import "testing"

func TestValidateBaseURLMetadataAddresses(t *testing.T) {
	for _, raw := range []string{"http://169.254.169.254", "http://[::ffff:169.254.169.254]", "http://[fd00:ec2::254]", "http://[fd00:ec2::254%25eth0]", "http://100.100.100.200", "http://METADATA.GOOGLE.INTERNAL./", "http://metadata.goog", "http://instance-data.ec2.internal"} {
		t.Run(raw, func(t *testing.T) {
			if ValidateBaseURL(raw) == nil {
				t.Fatal("metadata destination accepted")
			}
		})
	}
	for _, raw := range []string{"http://127.0.0.1:11434", "http://[::1]:11434", "http://10.0.0.1", "http://192.168.1.1", "http://[fd00::1]"} {
		t.Run(raw, func(t *testing.T) {
			if err := ValidateBaseURL(raw); err != nil {
				t.Fatal(err)
			}
		})
	}
}
