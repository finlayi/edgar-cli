package edgar

import "testing"

func TestShapeDataFields(t *testing.T) {
	shaped, err := shapeData([]map[string]any{
		{"a": 1, "b": 2, "c": 3},
		{"a": 4, "b": 5, "c": 6},
	}, []string{"a", "c"}, 0)
	if err != nil {
		t.Fatal(err)
	}

	rows := shaped.Data.([]map[string]any)
	if rows[0]["a"] != 1 || rows[0]["c"] != 3 || rows[0]["b"] != nil {
		t.Fatalf("unexpected projected row: %#v", rows[0])
	}
}

func TestShapeDataLimit(t *testing.T) {
	shaped, err := shapeData([]map[string]any{{"x": 1}, {"x": 2}, {"x": 3}}, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	rows := shaped.Data.([]map[string]any)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if shaped.MetaUpdates["total_count"] != 3 {
		t.Fatalf("total_count = %#v, want 3", shaped.MetaUpdates["total_count"])
	}
	if shaped.MetaUpdates["returned_count"] != 2 {
		t.Fatalf("returned_count = %#v, want 2", shaped.MetaUpdates["returned_count"])
	}
	if shaped.MetaUpdates["truncated"] != true {
		t.Fatalf("truncated = %#v, want true", shaped.MetaUpdates["truncated"])
	}
}
