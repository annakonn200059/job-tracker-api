package http

import (
	"encoding/json"
	"testing"
)

func TestOptional(t *testing.T) {
	type body struct {
		Notes    Optional[*string] `json:"notes"`
		Priority Optional[int16]   `json:"priority"`
	}

	tests := []struct {
		name         string
		json         string
		wantSet      bool
		wantNotes    *string
		wantPriority Optional[int16]
		wantErr      bool
	}{
		{name: "absent", json: `{}`},
		{name: "null clears", json: `{"notes": null}`, wantSet: true},
		{name: "value sets", json: `{"notes": "hi", "priority": 2}`, wantSet: true,
			wantNotes: new("hi"), wantPriority: Optional[int16]{Set: true, Value: 2}},
		{name: "null on non-nullable", json: `{"priority": null}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b body
			err := json.Unmarshal([]byte(tt.json), &b)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if b.Notes.Set != tt.wantSet {
				t.Errorf("Notes.Set = %v, want %v", b.Notes.Set, tt.wantSet)
			}
			if (b.Notes.Value == nil) != (tt.wantNotes == nil) ||
				(b.Notes.Value != nil && *b.Notes.Value != *tt.wantNotes) {
				t.Errorf("Notes.Value = %v, want %v", b.Notes.Value, tt.wantNotes)
			}
			if b.Priority != tt.wantPriority {
				t.Errorf("Priority = %+v, want %+v", b.Priority, tt.wantPriority)
			}
		})
	}
}
