package service

import "testing"

func TestParseWhatIsMyIPAddressFields(t *testing.T) {
	page := `
		<div>
			<p class="information"><span>Hostname:</span><span>95.64.61.22</span></p>
			<p class="information"><span>ISP:</span><span>Mobile Communication Company of Iran Plc</span></p>
			<p class="information"><span>Country:</span><span>Iran (Islamic Republic of)</span></p>
			<p class="information"><span>State/Region:</span><span>Hormozgan</span></p>
			<p class="information"><span>City:</span><span>Qeshm</span></p>
			<p class="information"><span>Latitude:</span><span>26.9581&nbsp;(26&deg; 57&prime; 29.22&Prime; N)</span></p>
		</div>
	`

	fields := parseWhatIsMyIPAddressFields(page)

	if got := fields["Hostname"]; got != "95.64.61.22" {
		t.Fatalf("Hostname = %q, want %q", got, "95.64.61.22")
	}
	if got := fields["ISP"]; got != "Mobile Communication Company of Iran Plc" {
		t.Fatalf("ISP = %q, want %q", got, "Mobile Communication Company of Iran Plc")
	}
	if got := fields["Country"]; got != "Iran (Islamic Republic of)" {
		t.Fatalf("Country = %q, want %q", got, "Iran (Islamic Republic of)")
	}
	if got := fields["State/Region"]; got != "Hormozgan" {
		t.Fatalf("State/Region = %q, want %q", got, "Hormozgan")
	}
	if got := fields["City"]; got != "Qeshm" {
		t.Fatalf("City = %q, want %q", got, "Qeshm")
	}
	if got := fields["Latitude"]; got != "26.9581 (26° 57′ 29.22″ N)" {
		t.Fatalf("Latitude = %q, want %q", got, "26.9581 (26° 57′ 29.22″ N)")
	}
}
