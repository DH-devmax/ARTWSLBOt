package features

import "testing"

func TestReadOnlyGroupConfigurationDoesNotGrantEditing(t *testing.T) {
	for _, d := range Catalog() {
		for _, field := range d.Fields {
			if !CanRead([]string{"groups:read"}, d, field.Key) || CanEdit([]string{"groups:read"}, d, field.Key) {
				t.Fatal("read and write privileges merged", d.ID, field.Key)
			}
			if CanRead(nil, d, field.Key) || CanRead([]string{"groups:read"}, d, "invented") {
				t.Fatal("ungranted or unknown field became readable", d.ID)
			}
		}
	}
}
