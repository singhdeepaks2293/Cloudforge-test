// Package courses holds the seeded Milestone 1 course catalogue.
// M2 replaces this in-memory list with PostgreSQL-backed course and
// lesson management (see migrations/001_init.sql).
package courses

import "encoding/json"

// Course is a single course in the catalogue.
type Course struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Track       string   `json:"track"`
	Description string   `json:"description"`
	Lessons     []string `json:"lessons"`
}

// LessonCount is the number of lessons in the course.
func (c Course) LessonCount() int { return len(c.Lessons) }

// courseJSON is the wire shape: lessons are exposed as a count, not the
// full lesson bodies, in list views; detail views include the titles.
type courseView struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Track       string   `json:"track"`
	Description string   `json:"description"`
	Lessons     []string `json:"lessons,omitempty"`
	LessonCount int      `json:"lesson_count"`
}

func (c Course) view(detail bool) courseView {
	v := courseView{ID: c.ID, Title: c.Title, Track: c.Track, Description: c.Description, LessonCount: len(c.Lessons)}
	if detail {
		v.Lessons = c.Lessons
	}
	return v
}

// MarshalJSON exposes lesson_count without the full lesson bodies.
func (c Course) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.view(false))
}

// View returns the detail representation of a course.
func (c Course) View() any { return c.view(true) }

// List returns the seeded catalogue in a stable order.
func List() []Course {
	return append([]Course(nil), seeded...)
}

// ListViews returns the list representation of the catalogue.
func ListViews() []any {
	out := make([]any, 0, len(seeded))
	for _, c := range seeded {
		out = append(out, c.view(false))
	}
	return out
}

// Get returns a course by ID.
func Get(id string) (Course, bool) {
	for _, c := range seeded {
		if c.ID == id {
			return c, true
		}
	}
	return Course{}, false
}

var seeded = []Course{
	{
		ID:    "vpc-fundamentals",
		Title: "VPC Fundamentals",
		Track: "Networking",
		Description: "Virtual private clouds, subnets, route tables, internet " +
			"gateways and NAT — build an isolated network the way AWS VPCs work, " +
			"on real Linux networking primitives.",
		Lessons: []string{
			"Cloud networking concepts",
			"VPCs and CIDR blocks",
			"Public and private subnets",
			"Route tables and internet gateways",
			"NAT gateways",
			"Security groups and network ACLs",
		},
	},
	{
		ID:    "iam-security",
		Title: "IAM & Security",
		Track: "Security",
		Description: "Users, groups, roles and policies. Learn least privilege " +
			"by writing policies and watching them allow or deny real operations.",
		Lessons: []string{
			"Users, groups and roles",
			"Writing IAM policies",
			"Least privilege in practice",
			"Roles vs long-lived credentials",
			"Troubleshooting access denied",
		},
	},
	{
		ID:    "ec2-basics",
		Title: "EC2 Basics",
		Track: "Compute",
		Description: "Launch, connect to and manage Linux instances. AMIs, key " +
			"pairs, disks and the instance lifecycle — backed by local KVM VMs.",
		Lessons: []string{
			"Instances and AMIs",
			"Key pairs and SSH",
			"Instance lifecycle",
			"Disks and EBS concepts",
			"Instance metadata and cloud-init",
		},
	},
}
