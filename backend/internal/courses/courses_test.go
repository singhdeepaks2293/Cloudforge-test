package courses

import "testing"

func TestSeededCatalogue(t *testing.T) {
	list := List()
	if len(list) != 3 {
		t.Fatalf("expected 3 seeded courses, got %d", len(list))
	}
	want := map[string]int{"vpc-fundamentals": 6, "iam-security": 5, "ec2-basics": 5}
	for _, c := range list {
		if want[c.ID] != c.LessonCount() {
			t.Errorf("%s: lesson count %d, want %d", c.ID, c.LessonCount(), want[c.ID])
		}
		if c.Title == "" || c.Track == "" {
			t.Errorf("%s: title/track must not be empty", c.ID)
		}
	}
	if _, ok := Get("vpc-fundamentals"); !ok {
		t.Fatal("vpc-fundamentals should be found")
	}
	if _, ok := Get("nope"); ok {
		t.Fatal("unknown course id must not be found")
	}
}
