package tflearn

// Course structure for Terraform Learning (master prompt §9).
//
// Level 1 — Fundamentals (15 lessons, as listed in §9), Level 2 —
// Intermediate (20), Level 3 — Advanced (20), Level 4 — AWS-style
// Infrastructure Projects (7). Every lesson carries the §9 shape:
// explanation, real-world use case, code example, guided exercise,
// expected output, common mistakes, knowledge check, completion
// tracking (progress is stored per user in the tflearn_progress
// collection; see the httpapi layer).
//
// Curriculum reuse: the existing curriculum Terraform track
// (curriculum/terraform/terraform.yaml, 7 lessons across 3 modules)
// already teaches the Level 1 core. Where a Level 1 lesson overlaps
// it, CurriculumLesson names the matching track lesson title — the
// content is NOT duplicated here as a second copy of that track; the
// tflearn lesson adds the §9 exercise / expected-output / mistakes /
// knowledge-check shape around it. Levels 2–4 have no counterpart in
// the existing curriculum and are new content that lives only here.

type Quiz struct {
	Question    string   `json:"question"`
	Options     []string `json:"options"`
	Correct     int      `json:"correct_answer"`
	Explanation string   `json:"explanation"`
}

type Lesson struct {
	ID               string   `json:"id"`
	Level            int      `json:"level"`
	Title            string   `json:"title"`
	Summary          string   `json:"summary"`
	Explanation      string   `json:"explanation"`
	UseCase          string   `json:"use_case"`
	CodeExample      string   `json:"code_example"`
	Exercise         string   `json:"exercise"`
	ExpectedOutput   string   `json:"expected_output"`
	Mistakes         []string `json:"common_mistakes"`
	Quiz             Quiz     `json:"knowledge_check"`
	CurriculumLesson string   `json:"curriculum_lesson,omitempty"`
	XP               int      `json:"xp"`
}

type Level struct {
	Level       int      `json:"level"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Lessons     []Lesson `json:"lessons"`
}

type Project struct {
	ID           string            `json:"id"`
	Level        int               `json:"level"`
	Title        string            `json:"title"`
	Description  string            `json:"description"`
	Objectives   []string          `json:"objectives"`
	Tasks        []string          `json:"tasks"`
	Criteria     []string          `json:"validation_criteria"`
	StarterFiles map[string]string `json:"starter_files"`
	XP           int               `json:"xp"`
}

// Levels returns the full course structure (levels 1–3).
func Levels() []Level { return []Level{level1, level2, level3} }

// Projects returns the Level 4 infrastructure projects.
func Projects() []Project { return level4Projects }

// FindLesson returns a lesson by id across all levels.
func FindLesson(id string) (Lesson, bool) {
	for _, l := range Levels() {
		for _, lesson := range l.Lessons {
			if lesson.ID == id {
				return lesson, true
			}
		}
	}
	return Lesson{}, false
}

// TotalLessons is the number of lessons across levels 1–3.
func TotalLessons() int {
	n := 0
	for _, l := range Levels() {
		n += len(l.Lessons)
	}
	return n
}

// SkillLevel maps completed-lesson counts to a displayed skill level.
// It is computed from real progress data, never canned.
func SkillLevel(completed, total int) string {
	if total <= 0 {
		return "Novice"
	}
	switch pct := completed * 100 / total; {
	case completed >= total:
		return "Expert"
	case pct >= 70:
		return "Advanced"
	case pct >= 35:
		return "Intermediate"
	case pct >= 10:
		return "Beginner"
	default:
		return "Novice"
	}
}

var level1 = Level{
	Level: 1, Title: "Fundamentals",
	Description: "Infrastructure as Code from first principles: what Terraform is, how it tracks state, and the init → validate → plan → apply → destroy workflow.",
	Lessons: []Lesson{
		{
			ID: "l1-01-iac", Level: 1, Title: "Infrastructure as Code",
			Summary:          "Why infrastructure should be written down, versioned, and reviewed like application code.",
			Explanation:      "Infrastructure as Code (IaC) means describing infrastructure — networks, servers, buckets, queues — in text files instead of creating it by clicking through a console. The files are versioned in git, reviewed in pull requests, and applied by a tool, so the same configuration produces the same infrastructure every time, and any change has a history and an author. Without IaC, infrastructure lives in one person's memory and in a console's click history; it cannot be reviewed, tested, or rebuilt reliably.",
			UseCase:          "A team onboards a new engineer: instead of a 40-step console runbook, they hand over a repository. The engineer runs terraform plan and sees exactly what exists; a new environment is a new workspace, not a new runbook.",
			CodeExample:      "# Not Terraform yet — the idea:\n# infrastructure is a file, not a memory.\n#\n# vpc \"main\" 10.0.0.0/16\n# subnet public 10.0.1.0/24\n# instance web (micro) in public subnet",
			Exercise:         "Write down, in plain text, the infrastructure for a two-tier web app (VPC, two subnets, one instance, one bucket). Keep it under 15 lines. Then compare: what did you leave ambiguous (sizes? names? regions?) that a tool would need to know?",
			ExpectedOutput:   "A short list naming each component and its key settings (CIDR blocks, instance size, bucket name). Ambiguities you found: exact sizes, naming, region, and how components connect — those are exactly what Terraform's language makes explicit.",
			Mistakes:         []string{"Treating IaC as 'scripts that click the console for you' — scripts are imperative steps; IaC is a desired state.", "Writing configuration nobody reviews — the review is the point; unreviewed IaC is just automation of mistakes."},
			Quiz:             Quiz{Question: "What is the core idea of Infrastructure as Code?", Options: []string{"Clicking faster in the console", "Describing desired infrastructure in versioned text files that a tool applies", "Writing shell scripts to create servers", "Buying more servers"}, Correct: 1, Explanation: "IaC describes the desired state in versioned files; a tool converges reality to that state."},
			CurriculumLesson: "Why Infrastructure as Code", XP: 10,
		},
		{
			ID: "l1-02-what-is-terraform", Level: 1, Title: "What is Terraform?",
			Summary:          "Terraform is a declarative IaC tool: you describe the end state, it works out the steps.",
			Explanation:      "Terraform is HashiCorp's open-source IaC tool. You write HCL (HashiCorp Configuration Language) files describing resources you want; Terraform compares that desired state with the real world and with its state file, produces a plan, and applies it. It is declarative: you say 'a VPC with this CIDR exists', not 'call CreateVpc, then wait, then call CreateSubnet'. Providers are plugins — the AWS provider talks to AWS, the CloudForge provider maps to CloudForge APIs — while Terraform core (plan/apply/state/graph) stays the same.",
			UseCase:          "A team manages a VPC, subnets, and an application across three environments. One Terraform codebase with variables per environment replaces three sets of console clicks and three runbooks.",
			CodeExample:      "terraform {\n  required_providers {\n    aws = {\n      source  = \"hashicorp/aws\"\n      version = \"~> 5.0\"\n    }\n  }\n}\n\nresource \"aws_vpc\" \"main\" {\n  cidr_block = \"10.0.0.0/16\"\n}",
			Exercise:         "In the editor, create a project from the 'VPC' template and read main.tf. Identify the terraform block, the provider block, and the resource block, and write one sentence explaining what each does.",
			ExpectedOutput:   "terraform block: pins provider versions. provider block: configures which cloud and region. resource block: declares one VPC with CIDR 10.0.0.0/16.",
			Mistakes:         []string{"Confusing Terraform with a cloud provider — Terraform is the tool; providers are plugins.", "Editing infrastructure in the console after Terraform manages it — that is drift, covered in Level 3."},
			Quiz:             Quiz{Question: "Terraform is best described as…", Options: []string{"An AWS service", "A declarative IaC tool that converges real infrastructure to a described desired state", "A container runtime", "A monitoring tool"}, Correct: 1, Explanation: "Terraform is declarative: you describe the end state and it plans and applies the steps."},
			CurriculumLesson: "Why Infrastructure as Code", XP: 10,
		},
		{
			ID: "l1-03-architecture", Level: 1, Title: "Terraform Architecture",
			Summary:          "Core, providers, state, and the dependency graph — the four parts that make a plan possible.",
			Explanation:      "Terraform core reads configuration, builds a dependency graph of resources (a subnet that references a VPC must wait for the VPC), and drives the workflow. Providers are separate binaries that implement the CRUD operations for one platform's resources. State is Terraform's record of what it manages: resource IDs, attributes, and dependencies. The graph plus state is what lets Terraform plan accurately — it knows what exists, what is declared, and what order to create things in. In CloudForge, the container runner executes the real Terraform binary (hashicorp/terraform:1.9.8) and the local provider maps cloudforge_* resources to CloudForge APIs.",
			UseCase:          "A plan shows a subnet being created before its VPC — impossible. The graph prevents it: references like cloudforge_subnet.public's vpc_id = cloudforge_vpc.main.id create an implicit dependency, so the VPC is always created first.",
			CodeExample:      "resource \"cloudforge_vpc\" \"main\" {\n  cidr_block = \"10.0.0.0/16\"\n}\n\nresource \"cloudforge_subnet\" \"public\" {\n  vpc_id     = cloudforge_vpc.main.id  # implicit dependency\n  cidr_block = \"10.0.1.0/24\"\n}",
			Exercise:         "Draw the dependency graph for a VPC, two subnets, a security group, and an instance that references the subnet and the security group. Which resource has no dependencies? Which is created last?",
			ExpectedOutput:   "VPC has no dependencies and is created first. Subnets and the security group depend on the VPC. The instance depends on the subnet and the security group, so it is created last.",
			Mistakes:         []string{"Assuming resources are created in file order — order comes from the graph, not the file.", "Creating circular references (A needs B, B needs A) — Terraform refuses to plan a cycle."},
			Quiz:             Quiz{Question: "What creates an implicit dependency between two resources?", Options: []string{"Writing them in the same file", "One resource referencing an attribute of the other", "Giving them the same name", "Running apply twice"}, Correct: 1, Explanation: "References between resources create implicit dependencies that order creation."},
			CurriculumLesson: "State, Plan and Apply", XP: 10,
		},
		{
			ID: "l1-04-providers", Level: 1, Title: "Providers",
			Summary:          "Providers are plugins that let Terraform talk to one platform; each has its own resources and configuration.",
			Explanation:      "A provider is a plugin binary that translates Terraform's generic resource operations into API calls for one platform — AWS, Azure, CloudForge, or hundreds of others. You declare required providers (with source and version constraints) in the terraform block and configure each provider in a provider block (region, endpoints, credentials). Providers are downloaded at terraform init. In CloudForge learning mode the AWS provider points at the local emulator only, and the CloudForge provider maps to local APIs; real AWS credentials are refused.",
			UseCase:          "A project uses two providers at once: the AWS provider for emulator-backed resources and the CloudForge provider for lab resources — same workflow, same state file, two platforms.",
			CodeExample:      "terraform {\n  required_providers {\n    aws = {\n      source  = \"hashicorp/aws\"\n      version = \"~> 5.0\"\n    }\n  }\n}\n\nprovider \"aws\" {\n  region = \"us-east-1\"\n}",
			Exercise:         "Open the provider documentation (GET /api/v1/terraform-learning/provider) and list three cloudforge_* resource types with the CloudForge API each maps to. Then add a provider block for the CloudForge provider to your project.",
			ExpectedOutput:   "Three types with their mapped APIs, e.g. cloudforge_vpc → POST /labs/{id}/resources (kind vpc); cloudforge_sqs_queue → POST /aws/sqs/queues; cloudforge_iam_role → POST /aws/iam/roles.",
			Mistakes:         []string{"Pinning no version and getting a breaking provider upgrade on the next init.", "Putting real credentials in a provider block — credentials never belong in configuration."},
			Quiz:             Quiz{Question: "What does terraform init do with providers?", Options: []string{"It deletes them", "It downloads and installs the declared provider plugins", "It applies your resources", "It creates an AWS account"}, Correct: 1, Explanation: "init installs the provider plugins declared in required_providers."},
			CurriculumLesson: "The Terraform Language: Resources and Data", XP: 10,
		},
		{
			ID: "l1-05-resources", Level: 1, Title: "Resources",
			Summary:          "A resource block declares one managed object: its type, name, and arguments.",
			Explanation:      "A resource block is the basic unit of Terraform: resource \"<type>\" \"<name>\" { arguments }. The type selects the provider's resource (aws_vpc, cloudforge_subnet); the name is local to your configuration and is how other blocks refer to it (aws_vpc.main.id). Arguments set its properties; after apply, Terraform records the real ID and attributes in state. Changing an argument that cannot be updated in place forces replacement — destroy then create — which the plan shows explicitly.",
			UseCase:          "You need two identical subnets in different availability zones: two resource blocks differing only in name and CIDR. Later, count/for_each (Level 2) removes the duplication — but the two-block version is the honest starting point.",
			CodeExample:      "resource \"aws_vpc\" \"main\" {\n  cidr_block = \"10.0.0.0/16\"\n\n  tags = {\n    Name = \"main\"\n  }\n}\n\nresource \"aws_subnet\" \"public\" {\n  vpc_id     = aws_vpc.main.id\n  cidr_block = \"10.0.1.0/24\"\n}",
			Exercise:         "Add a second subnet (10.0.2.0/24, name 'private') to your project and run local validate. Then run a plan preview and check it shows one new create.",
			ExpectedOutput:   "Validation passes with no errors. The plan preview lists aws_subnet.private (or cloudforge_subnet.private) as create, and the existing resources as no-op.",
			Mistakes:         []string{"Reusing the same local name for two resources of one type — names must be unique per type.", "Treating the local name as the cloud name — the local name only exists in your files; tags/arguments set the real name."},
			Quiz:             Quiz{Question: "In resource \"aws_vpc\" \"main\", what is \"main\"?", Options: []string{"The VPC's name in AWS", "The local label used to refer to the resource in configuration", "The region", "The provider version"}, Correct: 1, Explanation: "The second label is the local name; other blocks refer to it as aws_vpc.main."},
			CurriculumLesson: "The Terraform Language: Resources and Data", XP: 10,
		},
		{
			ID: "l1-06-data-sources", Level: 1, Title: "Data Sources",
			Summary:          "Data sources read existing infrastructure instead of managing it.",
			Explanation:      "A data block reads information Terraform does not manage: the latest AMI, an existing VPC, the current region. Data sources are read during plan (and refresh), never created or destroyed, and their attributes can feed resource arguments. The rule of thumb: if Terraform should own it, use a resource; if it already exists and you only need to look at it, use a data source.",
			UseCase:          "Your instance needs the latest Amazon Linux AMI. Hardcoding an AMI ID rots; a data source for the AMI (filtered by name and owner) always resolves to the current image at plan time.",
			CodeExample:      "data \"aws_ami\" \"latest\" {\n  most_recent = true\n\n  filter {\n    name   = \"name\"\n    values = [\"amzn2-ami-hvm-*\"]\n  }\n}\n\nresource \"aws_instance\" \"web\" {\n  ami           = data.aws_ami.latest.id\n  instance_type = \"t3.micro\"\n}",
			Exercise:         "Add a data source to your project that reads your VPC (data \"aws_vpc\" \"main\" with a filter on its CIDR or ID) and use its id in a subnet's vpc_id. Validate locally.",
			ExpectedOutput:   "Validation passes. The subnet references data.aws_vpc.main.id, and the plan preview treats the data source as read-only (no create for it).",
			Mistakes:         []string{"Using a data source for something Terraform should manage — it will never be created, updated, or destroyed.", "Depending on data that changes every plan (e.g. 'latest' AMI) without realising it can force replacements downstream."},
			Quiz:             Quiz{Question: "When should you use a data source instead of a resource?", Options: []string{"When you want Terraform to create it", "When the infrastructure already exists and you only need to read it", "When it is cheaper", "Never"}, Correct: 1, Explanation: "Data sources read existing infrastructure; resources manage it."},
			CurriculumLesson: "The Terraform Language: Resources and Data", XP: 10,
		},
		{
			ID: "l1-07-variables", Level: 1, Title: "Variables",
			Summary:          "Variables parameterise configuration so one codebase serves many environments.",
			Explanation:      "Variables are inputs to a module: declared with a variable block (type, description, default, validation), referenced as var.name, and set via terraform.tfvars files, -var flags, or environment variables (TF_VAR_name). Precedence matters: environment variables lose to tfvars, which lose to -var flags. Marking a variable sensitive hides its value from plan/apply output — but it is still stored in state in plain text unless the backend encrypts it, so secrets need a real secrets manager.",
			UseCase:          "One VPC module serves dev, staging, and prod: the CIDR, environment name, and instance size are variables, set per environment in a tfvars file. The code is identical; only the inputs differ.",
			CodeExample:      "variable \"cidr_block\" {\n  description = \"CIDR for the VPC\"\n  type        = string\n  default     = \"10.0.0.0/16\"\n}\n\nvariable \"environment\" {\n  description = \"Deployment environment\"\n  type        = string\n\n  validation {\n    condition     = contains([\"dev\", \"staging\", \"prod\"], var.environment)\n    error_message = \"environment must be dev, staging or prod.\"\n  }\n}\n\nresource \"aws_vpc\" \"main\" {\n  cidr_block = var.cidr_block\n}",
			Exercise:         "Add a variable 'instance_type' (default 'micro') to your project, use it in the instance resource, then set it to 'small' in terraform.tfvars and run a plan preview. What changes?",
			ExpectedOutput:   "The plan preview shows the instance as update (or create, if no state exists yet) with the new type; the VPC is no-op. The tfvars value wins over the default.",
			Mistakes:         []string{"Leaving every variable without a type or description — the module becomes unreadable and unvalidatable.", "Putting secrets in tfvars and committing the file — tfvars with secrets belongs in .gitignore."},
			Quiz:             Quiz{Question: "Which wins when a variable is set in both terraform.tfvars and with -var on the command line?", Options: []string{"terraform.tfvars", "The -var flag", "The default value", "The environment variable"}, Correct: 1, Explanation: "-var flags have the highest precedence, then tfvars files, then environment variables, then defaults."},
			CurriculumLesson: "The Terraform Language: Resources and Data", XP: 10,
		},
		{
			ID: "l1-08-outputs", Level: 1, Title: "Outputs",
			Summary:          "Outputs expose values after apply: IDs, addresses, and wiring between modules.",
			Explanation:      "An output block exposes a value from a module after apply — a VPC ID, a subnet's CIDR, a bucket name. Outputs are how modules hand results to their caller and how operators read what was created (terraform output). Sensitive outputs are hidden from CLI output. Outputs are also the mechanism for cross-stack references: one stack's output becomes another's input via remote state (Level 3).",
			UseCase:          "The networking module outputs vpc_id and subnet IDs; the compute module takes them as variables. Neither module knows the other's internals — the outputs are the contract.",
			CodeExample:      "output \"vpc_id\" {\n  description = \"ID of the main VPC\"\n  value       = aws_vpc.main.id\n}\n\noutput \"public_subnet_id\" {\n  description = \"ID of the public subnet\"\n  value       = aws_subnet.public.id\n}",
			Exercise:         "Add outputs for your VPC id and your public subnet id to outputs.tf, then run a plan preview. Which outputs does the preview list?",
			ExpectedOutput:   "The plan preview (and a real plan) lists the two outputs with their planned values — the VPC id is known only after apply, so it may show as '(known after apply)' in a real plan.",
			Mistakes:         []string{"Outputting secrets without sensitive = true — they print in logs and CI output.", "Outputting huge objects (whole resources) instead of the specific IDs callers need."},
			Quiz:             Quiz{Question: "What are outputs for?", Options: []string{"Deleting resources", "Exposing values (IDs, addresses) from a module after apply", "Installing providers", "Formatting code"}, Correct: 1, Explanation: "Outputs expose values after apply and form the contract between modules."},
			CurriculumLesson: "The Terraform Language: Resources and Data", XP: 10,
		},
		{
			ID: "l1-09-config-files", Level: 1, Title: "Configuration Files",
			Summary:          "How a project is laid out: main.tf, variables.tf, outputs.tf, providers.tf, and tfvars.",
			Explanation:      "Terraform reads every .tf file in a directory as one configuration; the split into files is convention, not syntax. The standard layout: main.tf (resources and data), variables.tf (inputs), outputs.tf (outputs), providers.tf (provider configuration and required_providers), versions.tf (Terraform and provider version constraints), terraform.tfvars (values for variables). Modules live under modules/<name>/. Keeping files small and named by purpose is what makes a project reviewable; one 2,000-line main.tf is a smell.",
			UseCase:          "A reviewer opens a pull request and can find the VPC definition in main.tf, the inputs in variables.tf, and the values in terraform.tfvars — without reading the whole project.",
			CodeExample:      "main.tf         # resources: vpc, subnets, instance\nvariables.tf    # variable \"cidr_block\", variable \"environment\"\noutputs.tf      # output \"vpc_id\"\nproviders.tf    # provider \"aws\" { region = ... }\nversions.tf     # required_version = \">= 1.5\"\nterraform.tfvars # cidr_block = \"10.1.0.0/16\"\nmodules/        # modules/network/, modules/compute/",
			Exercise:         "In your project, move the VPC resource into main.tf, the variables into variables.tf, the outputs into outputs.tf, and the provider block into providers.tf. Run local validate after each move — it must stay green.",
			ExpectedOutput:   "Validation passes at every step (file layout does not change semantics). The file explorer shows the standard layout.",
			Mistakes:         []string{"One giant main.tf with everything — unreviewable and unmergeable.", "Committing terraform.tfvars with environment-specific secrets instead of per-environment tfvars in .gitignore."},
			Quiz:             Quiz{Question: "Which file conventionally holds variable declarations?", Options: []string{"main.tf", "variables.tf", "outputs.tf", "providers.tf"}, Correct: 1, Explanation: "variables.tf holds variable blocks; terraform.tfvars holds their values."},
			CurriculumLesson: "Modules and Reuse", XP: 10,
		},
		{
			ID: "l1-10-init", Level: 1, Title: "terraform init",
			Summary:          "Initialise a working directory: install providers, configure the backend, prepare modules.",
			Explanation:      "terraform init prepares a directory: it downloads the provider plugins declared in required_providers, installs modules from their sources, and configures the backend (where state lives). It must run before any other command, and again whenever providers, modules, or backend configuration change. Init does not touch infrastructure — it only prepares the local working directory — so it is always safe to re-run. In CloudForge, init runs inside the container (hashicorp/terraform:1.9.8) against the lab workdir.",
			UseCase:          "A new engineer clones the repository and runs terraform init: providers download, the backend is configured, and the directory is ready to plan. Nothing in the cloud changes.",
			CodeExample:      "$ terraform init\n\nInitializing the backend...\nInitializing provider plugins...\n- Finding hashicorp/aws versions matching \"~> 5.0\"...\n- Installing hashicorp/aws v5.70.0...\n\nTerraform has been successfully initialized!",
			Exercise:         "Link your project to a lab, then run init from the Executions tab. Read the log: which providers were installed, and what backend was configured? (In this sandbox without Docker, init honestly returns 503 terraform_unavailable — record that instead of a success.)",
			ExpectedOutput:   "A run record with status SUCCEEDED and a log naming the installed providers (or, without Docker, a 503 terraform_unavailable run with the honest reason).",
			Mistakes:         []string{"Running plan before init and reading the error as a configuration problem — init first, always.", "Committing the .terraform directory — it is a local cache, generated by init."},
			Quiz:             Quiz{Question: "What does terraform init do?", Options: []string{"Creates all resources", "Installs providers and modules and configures the backend", "Deletes state", "Formats code"}, Correct: 1, Explanation: "init prepares the working directory: providers, modules, backend. It touches no infrastructure."},
			CurriculumLesson: "Plan, Approve, Apply and Destroy", XP: 10,
		},
		{
			ID: "l1-11-validate", Level: 1, Title: "terraform validate",
			Summary:          "Validate checks configuration syntax and internal consistency — fast, offline, before planning.",
			Explanation:      "terraform validate checks that the configuration is syntactically valid and internally consistent: blocks are well-formed, references resolve, types check. It needs init to have run (providers must be installed for full checks) but it contacts no cloud — it is fast and safe to run on every save and in CI. CloudForge also runs a local validation in the editor without Docker: brace balance, block structure, undeclared variables and resources, forbidden hosts and embedded credentials. Local validation is not a substitute for the binary's validate, and the UI labels it as local.",
			UseCase:          "A pull request's CI runs terraform fmt -check and terraform validate: a typo in a resource reference is caught in seconds, before anyone reviews the plan.",
			CodeExample:      "$ terraform validate\nSuccess! The configuration is valid.",
			Exercise:         "Deliberately break your configuration — reference var.undefined in the VPC block — and run local validate. Read the error, fix it, and validate again until it is green.",
			ExpectedOutput:   "First run: an error naming var.undefined (and its line). After the fix: 'valid' with no errors.",
			Mistakes:         []string{"Skipping validate because 'plan will catch it' — validate is seconds, plan is minutes and needs credentials.", "Treating a green validate as proof the plan will succeed — validate checks syntax, not the real world."},
			Quiz:             Quiz{Question: "What does terraform validate check?", Options: []string{"Whether AWS accepts the resources", "Syntax and internal consistency of the configuration, without contacting the cloud", "The cost of the infrastructure", "Whether the state file is locked"}, Correct: 1, Explanation: "validate is a fast offline syntax/consistency check; it does not contact the cloud."},
			CurriculumLesson: "Plan, Approve, Apply and Destroy", XP: 10,
		},
		{
			ID: "l1-12-format", Level: 1, Title: "terraform fmt",
			Summary:          "Formatting is a shared convention: canonical indentation and spacing, enforced automatically.",
			Explanation:      "terraform fmt rewrites configuration files to a canonical style: two-space indentation, aligned = signs in consecutive attributes, consistent spacing. It changes no semantics — only whitespace. Running fmt -check in CI fails the build on unformatted code, so style arguments never reach review. CloudForge's editor has a Format action that applies the same idea locally (indentation by brace depth, spacing around =) and shows what changed.",
			UseCase:          "Two engineers edit the same file: because both run fmt, the diff shows only their real change — no whitespace noise.",
			CodeExample:      "# Before fmt:\nresource \"aws_vpc\" \"main\" {\ncidr_block=\"10.0.0.0/16\"\n}\n\n# After fmt:\nresource \"aws_vpc\" \"main\" {\n  cidr_block = \"10.0.0.0/16\"\n}",
			Exercise:         "Paste unformatted HCL (no indentation, no spaces around =) into main.tf and run Format. Compare the before/after and confirm nothing but whitespace changed.",
			ExpectedOutput:   "Formatted file with 2-space indentation and 'key = value' spacing; validation still green; no tokens changed.",
			Mistakes:         []string{"Hand-formatting in review comments instead of running fmt — let the tool own style.", "Formatting only some files — run fmt recursively (fmt -recursive) across the project."},
			Quiz:             Quiz{Question: "What does terraform fmt change?", Options: []string{"Resource types", "Only whitespace — indentation and spacing", "Variable values", "The state file"}, Correct: 1, Explanation: "fmt is a whitespace-only canonical formatter; it never changes semantics."},
			CurriculumLesson: "Plan, Approve, Apply and Destroy", XP: 10,
		},
		{
			ID: "l1-13-plan", Level: 1, Title: "terraform plan",
			Summary:          "Plan is the review step: read the diff before anything changes.",
			Explanation:      "terraform plan compares desired state (configuration), recorded state (state file), and real state (refreshed from the provider) and prints a diff: resources to create (+), modify (~), destroy (-), or replace (-/+). Plan changes nothing — it is the safe, reviewable step, and the step that protects you: a plan that destroys a database is a plan you stop and investigate. CloudForge requires an explicit approval of a succeeded plan before apply (M7: 1-hour TTL), and the Learning section adds a local plan preview (static diff, labelled as such) you can read without Docker.",
			UseCase:          "A pull request's CI posts the plan as a comment: reviewers see exactly what will change before approving the merge — the plan is the change request.",
			CodeExample:      "$ terraform plan\n\nTerraform will perform the following actions:\n\n  # aws_vpc.main will be created\n  + resource \"aws_vpc\" \"main\" {\n      + cidr_block = \"10.0.0.0/16\"\n    }\n\nPlan: 1 to add, 0 to change, 0 to destroy.",
			Exercise:         "Run a plan preview on your project. For each change, say in one sentence what it does and whether you would approve it. Then change the VPC CIDR and preview again — what does the diff show now?",
			ExpectedOutput:   "First preview: creates listed, everything else no-op. After the CIDR change: the VPC shows as destroy-and-create (forced replacement) — a change you would stop and think about, because it destroys the VPC.",
			Mistakes:         []string{"Applying without reading the plan — the plan is the only thing standing between a typo and an outage.", "Treating 'Plan: 0 to add, 0 to change, 0 to destroy' as a failure — it means reality already matches the configuration, which is the goal."},
			Quiz:             Quiz{Question: "What does terraform plan do?", Options: []string{"Creates the resources", "Shows the diff (create/update/destroy) without changing anything", "Deletes the state file", "Installs providers"}, Correct: 1, Explanation: "plan is a read-only diff: it shows what would change and changes nothing."},
			CurriculumLesson: "Plan, Approve, Apply and Destroy", XP: 10,
		},
		{
			ID: "l1-14-apply", Level: 1, Title: "terraform apply",
			Summary:          "Apply executes an approved plan — and only an approved plan.",
			Explanation:      "terraform apply executes a plan: it creates, updates, and destroys resources in dependency order and writes the results to state. In CloudForge, apply is never automatic: it requires an explicit approval of a succeeded, unexpired plan, plus a confirmation in the UI. Apply with no plan, an expired plan, or no confirmation is refused (409). After apply, state is the record of what exists — inspect it before trusting anything else. A failed apply leaves partial state: some resources exist, some do not; the next plan shows the gap and the next apply closes it.",
			UseCase:          "A reviewed plan is approved by a second person, then applied in a maintenance window. The apply log is the audit trail of what actually changed.",
			CodeExample:      "$ terraform apply plan.tfplan\n\naws_vpc.main: Creating...\naws_vpc.main: Creation complete after 2s [id=vpc-0123]\n\nApply complete! Resources: 1 added, 0 changed, 0 destroyed.",
			Exercise:         "Try to apply your project without an approved plan and without confirmation. Record the refusal (409). Then approve a plan preview and confirm — in this sandbox the container apply honestly returns 503 terraform_unavailable; record that instead of a success.",
			ExpectedOutput:   "Apply without approval/confirmation: 409 with a clear reason. With approval: the execution is created (or, without Docker, a 503 terraform_unavailable run — never a faked success).",
			Mistakes:         []string{"Running apply -auto-approve in a shared environment — auto-approve skips the only review step.", "Applying a plan generated from different configuration than what is on disk — always plan and apply from the same files."},
			Quiz:             Quiz{Question: "In CloudForge, what does apply require?", Options: []string{"Nothing — it always runs", "An approved, unexpired plan plus explicit confirmation", "Only a confirmation", "Only an approved plan"}, Correct: 1, Explanation: "Apply needs both: an approved, unexpired plan and an explicit confirmation. Either missing is a 409."},
			CurriculumLesson: "Plan, Approve, Apply and Destroy", XP: 10,
		},
		{
			ID: "l1-15-destroy", Level: 1, Title: "terraform destroy",
			Summary:          "Destroy removes everything Terraform manages — the cleanup step, also confirmation-gated.",
			Explanation:      "terraform destroy plans and applies the removal of every resource in state. It is the cleanup step for a lab, a test environment, or a decommission — and it is destructive, so CloudForge gates it behind an explicit confirmation just like apply, and shows the plan (what will be destroyed) before anything is removed. Destroy does not delete the configuration or the state file's history: the state versions remain, so you can see what existed. Orphaned resources — created outside Terraform or left by a failed destroy — keep billing and keep showing up in drift checks until removed.",
			UseCase:          "A lab ends: destroy removes the VPC, subnets, and instance, the state records the removal, and the next plan shows nothing to do. The environment is clean for the next learner.",
			CodeExample:      "$ terraform destroy\n\nTerraform will perform the following actions:\n\n  # aws_vpc.main will be destroyed\n  - resource \"aws_vpc\" \"main\" {\n      - cidr_block = \"10.0.0.0/16\"\n    }\n\nPlan: 0 to add, 0 to change, 1 to destroy.\n\nDo you really want to destroy all resources? (yes/no)",
			Exercise:         "Run a plan preview after removing all resources from your configuration. What does it show? Then attempt destroy without confirmation and record the refusal.",
			ExpectedOutput:   "The preview shows every recorded resource as delete. Destroy without confirmation: 409 confirm_required — nothing is removed.",
			Mistakes:         []string{"Destroying without reading what will be destroyed — stateful resources (databases, buckets with data) are gone for good.", "Deleting the state file to 'clean up' — without state, Terraform cannot destroy what it no longer knows about; those resources become orphans."},
			Quiz:             Quiz{Question: "What does terraform destroy remove?", Options: []string{"Only the configuration files", "Every resource recorded in state", "Only subnets", "The Terraform binary"}, Correct: 1, Explanation: "destroy removes everything in state — which is why it is confirmation-gated and why the state file must never be deleted to 'clean up'."},
			CurriculumLesson: "VPC as Code", XP: 10,
		},
	},
}

var level2 = Level{
	Level: 2, Title: "Intermediate",
	Description: "State backends, workspaces, modules, expressions, and the workflow habits that make Terraform safe for teams.",
	Lessons: []Lesson{
		{
			ID: "l2-01-state-backends", Level: 2, Title: "State Backends",
			Summary:        "Where state lives: local files for learning, remote backends with locking for teams.",
			Explanation:    "By default state is a local terraform.tfstate file. That is fine for learning and fatal for teams: the file is lost with the laptop, and two applies can race and corrupt it. A remote backend (S3 with DynamoDB locking in AWS; the CloudForge learning state store here) keeps state in one place, versions it, and locks it during operations so only one apply runs at a time. Backend configuration is set in the terraform block and takes effect at init; changing backends migrates state, which init asks you to confirm.",
			UseCase:        "Two engineers apply at once. With a local file, the second apply overwrites the first's state and resources become orphaned. With a remote backend and locking, the second apply waits or fails cleanly.",
			CodeExample:    "terraform {\n  backend \"s3\" {\n    bucket         = \"my-tf-state\"\n    key            = \"network/terraform.tfstate\"\n    region         = \"us-east-1\"\n    dynamodb_table = \"tf-locks\"\n  }\n}",
			Exercise:       "Lock your project's state from the State tab, then try to save a file while it is locked. What happens? Unlock it afterwards.",
			ExpectedOutput: "While locked, saves and executions are refused with 409 state_locked naming the lock holder. After unlock, the save succeeds.",
			Mistakes:       []string{"Sharing a state file over email or a shared drive — that is not a backend.", "Disabling locking to 'fix' a stuck lock — remove the specific stale lock after checking nobody is applying."},
			Quiz:           Quiz{Question: "Why does a team need a remote backend with locking?", Options: []string{"It is faster", "So state is not lost with one laptop and two applies cannot race", "Remote state is encrypted by default", "Local state cannot be read"}, Correct: 1, Explanation: "State is the record of what Terraform manages; losing it or racing two applies corrupts that record."},
			XP:             10,
		},
		{
			ID: "l2-02-workspaces", Level: 2, Title: "Workspaces",
			Summary:        "One configuration, multiple state files: workspaces for dev/staging/prod.",
			Explanation:    "Workspaces let one configuration manage several environments: each workspace has its own state file, selected with terraform workspace select, and the current workspace name is available as terraform.workspace so configuration can vary (e.g. instance size per workspace). Workspaces are a lightweight mechanism — they share the backend and the code — and they break down when environments genuinely diverge; then separate directories or separate stacks are the honest answer.",
			UseCase:        "dev, staging, and prod share the VPC module. Three workspaces, one codebase; terraform.workspace picks the CIDR and size per environment.",
			CodeExample:    "locals {\n  env_sizes = {\n    dev     = \"micro\"\n    staging = \"small\"\n    prod    = \"large\"\n  }\n}\n\nresource \"aws_instance\" \"web\" {\n  instance_type = local.env_sizes[terraform.workspace]\n}",
			Exercise:       "In your project, add a local that maps environment names to instance sizes and use terraform.workspace to select one. What would go wrong if a new workspace name had no entry in the map?",
			ExpectedOutput: "A lookup error at plan time: the map has no key for the new workspace. The fix is a default (lookup with a fallback) or an explicit entry per workspace.",
			Mistakes:       []string{"Using workspaces for environments that have diverged — the shared code becomes a pile of conditionals.", "Forgetting which workspace is selected and applying prod changes to dev (or the reverse)."},
			Quiz:           Quiz{Question: "What does terraform.workspace give you in configuration?", Options: []string{"The AWS region", "The name of the currently selected workspace", "The provider version", "The state file size"}, Correct: 1, Explanation: "terraform.workspace is the current workspace name, usable in expressions to vary behaviour per environment."},
			XP:             10,
		},
		{
			ID: "l2-03-modules-authoring", Level: 2, Title: "Authoring Modules",
			Summary:        "A module is a package of configuration: inputs in, outputs out, internals hidden.",
			Explanation:    "A module is a directory of .tf files with a clear contract: variables are inputs, outputs are results, and everything else is internal. A good module does one thing (a VPC, a compute cluster), validates its inputs, documents its variables, and exposes only the outputs callers need. Modules are called with a module block (source = local path or registry address) and can be versioned like any dependency.",
			UseCase:        "Every team needs a VPC with public and private subnets. One reviewed network module is used by ten projects instead of ten copy-pasted VPC definitions drifting apart.",
			CodeExample:    "# modules/network/main.tf\nvariable \"cidr_block\" { type = string }\nvariable \"azs\"        { type = list(string) }\n\nresource \"aws_vpc\" \"this\" {\n  cidr_block = var.cidr_block\n}\n\noutput \"vpc_id\" { value = aws_vpc.this.id }\n\n# root main.tf\nmodule \"network\" {\n  source     = \"./modules/network\"\n  cidr_block = \"10.0.0.0/16\"\n  azs        = [\"cf-local-1a\", \"cf-local-1b\"]\n}",
			Exercise:       "Extract your VPC and subnets into modules/network/ (main.tf, variables.tf, outputs.tf) and call it from root main.tf. Validate locally after the move.",
			ExpectedOutput: "Validation passes; the root configuration is a single module block; the module's outputs expose vpc_id and subnet IDs.",
			Mistakes:       []string{"A module that takes 30 variables — it is not a reusable abstraction, it is a copy of the caller.", "Exposing internal resource IDs as outputs 'just in case' — outputs are a contract; keep them minimal."},
			Quiz:           Quiz{Question: "What is a Terraform module?", Options: []string{"A single resource", "A reusable package of configuration with inputs and outputs", "A provider plugin", "A state backend"}, Correct: 1, Explanation: "Modules package configuration for reuse, with variables as inputs and outputs for wiring."},
			XP:             10,
		},
		{
			ID: "l2-04-module-registry", Level: 2, Title: "Using Modules from a Registry",
			Summary:        "Published modules: versioned, reviewed, and pinned — with the risks of trusting other people's code.",
			Explanation:    "Public and private module registries publish versioned modules (e.g. terraform-aws-modules/vpc/aws) that you reference by source and version. Pinning a version is mandatory: an unpinned module upgrades silently on the next init. Review a module before using it — its inputs, its defaults (does it create a NAT gateway that costs money?), and its outputs. In CloudForge there is no module registry yet; local modules under modules/ are the learning equivalent, and the same pinning discipline applies to provider versions.",
			UseCase:        "A team adopts the community VPC module at version 5.0.0. The pin means next month's init installs exactly 5.0.0, not whatever is newest.",
			CodeExample:    "module \"vpc\" {\n  source  = \"terraform-aws-modules/vpc/aws\"\n  version = \"5.0.0\"\n\n  name = \"main\"\n  cidr = \"10.0.0.0/16\"\n}",
			Exercise:       "Pick a module you would want from a registry (VPC, S3, or EKS). Write down its source, the version you would pin, and three things you would check in its README before using it.",
			ExpectedOutput: "A pinned source + version, and checks such as: what resources it creates, what it costs, what inputs are required, and what outputs it exposes.",
			Mistakes:       []string{"Using a module without a version pin — the next init can pull a breaking change.", "Trusting a module's defaults without reading them — defaults create real (billable) resources."},
			Quiz:           Quiz{Question: "Why pin a module version?", Options: []string{"To make init faster", "So the next init installs exactly the reviewed version, not an unreviewed newer one", "To reduce state size", "Pinning is not needed"}, Correct: 1, Explanation: "A version pin makes module upgrades an explicit, reviewed decision."},
			XP:             10,
		},
		{
			ID: "l2-05-count-foreach", Level: 2, Title: "count and for_each",
			Summary:        "Create many similar resources from one block — and know which meta-argument to reach for.",
			Explanation:    "count creates N copies of a resource (count.index selects the copy); for_each creates one copy per key in a map or set (each.key / each.value). Prefer for_each when copies have identities (subnets per AZ, users per name): removing one map entry removes exactly that copy. count is for identical copies where order does not matter; removing an element from the middle of a count list shifts every later index and can force replacements. Both are meta-arguments, written in the resource block alongside its arguments.",
			UseCase:        "Three subnets, one per AZ: for_each over a map of AZ → CIDR. Adding a fourth AZ adds one subnet; removing one removes exactly that subnet.",
			CodeExample:    "variable \"subnets\" {\n  type = map(string)\n  default = {\n    \"cf-local-1a\" = \"10.0.1.0/24\"\n    \"cf-local-1b\" = \"10.0.2.0/24\"\n  }\n}\n\nresource \"aws_subnet\" \"this\" {\n  for_each   = var.subnets\n  vpc_id     = aws_vpc.main.id\n  cidr_block = each.value\n}",
			Exercise:       "Convert your two subnet blocks into one for_each block over a map. Run a plan preview — does it show creates, or does it want to destroy and recreate? (With no recorded state yet, both show as creates; with state, the addresses change, which is the lesson: refactoring resource addresses needs moved blocks, Level 3.)",
			ExpectedOutput: "The preview shows the subnets under their new addresses. If state already recorded the old addresses, a real plan would show destroy+create — the reason moved blocks exist.",
			Mistakes:       []string{"Using count for named things — deleting the middle element renumbers everything after it.", "Mixing count and for_each in one block — Terraform forbids it."},
			Quiz:           Quiz{Question: "When is for_each better than count?", Options: []string{"Always", "When each copy has its own identity (a name or AZ) so removing one removes only that one", "When you want more copies", "Never"}, Correct: 1, Explanation: "for_each keys copies by identity; count indexes by position, so removals shift later copies."},
			XP:             10,
		},
		{
			ID: "l2-06-conditionals", Level: 2, Title: "Conditionals and Functions",
			Summary:        "Expressions: condition ? a : b, and the built-in functions that transform values.",
			Explanation:    "Terraform's expression language has conditionals (condition ? value-if-true : value-if-false), operators, and over a hundred built-in functions: lookup, merge, concat, format, cidrsubnet, jsonencode, try, coalesce. Conditionals choose between two values; count = var.enabled ? 1 : 0 conditionally creates a resource. Functions keep expressions readable — but an expression that needs three nested functions is a sign to use a local value with a clear name instead.",
			UseCase:        "A NAT gateway is created only in prod: count = var.environment == \"prod\" ? 1 : 0. Dev stays cheap; prod gets the gateway.",
			CodeExample:    "locals {\n  name_prefix = \"${var.environment}-main\"\n}\n\nresource \"aws_s3_bucket\" \"logs\" {\n  bucket = \"${local.name_prefix}-logs\"\n}",
			Exercise:       "Make your instance's name conditional on the environment (prod gets a '-prod' suffix, otherwise plain). Validate locally.",
			ExpectedOutput: "Validation passes; the conditional is a valid expression and the instance name varies by environment.",
			Mistakes:       []string{"Nesting three conditionals in one expression — extract a local with a name that explains the rule.", "Using a conditional to pick between two resource types — Terraform cannot conditionally choose a type; use count on two blocks instead."},
			Quiz:           Quiz{Question: "How do you conditionally create a resource?", Options: []string{"With an if statement", "With count = condition ? 1 : 0", "By deleting the block", "With a provider flag"}, Correct: 1, Explanation: "count driven by a conditional is the standard way to create a resource only sometimes."},
			XP:             10,
		},
		{
			ID: "l2-07-locals", Level: 2, Title: "Locals",
			Summary:        "Named intermediate values: locals make complex expressions readable and single-sourced.",
			Explanation:    "A locals block defines named values computed from variables, data, and resources: locals { name_prefix = \"${var.environment}-main\" }. Locals are not inputs (variables) or outputs — they are internal shorthand. Use them when an expression is repeated, when it needs a name to be understood, or when several resources must agree on one computed value (tags, names, CIDR math). If a value is needed by the caller, it is a variable; if it is needed after apply, it is an output; otherwise it is a local.",
			UseCase:        "Every resource is tagged Environment, Team, and CostCenter. One locals block computes the tag map; every resource merges it. The tags change in one place.",
			CodeExample:    "locals {\n  common_tags = {\n    Environment = var.environment\n    Team        = \"platform\"\n    ManagedBy   = \"terraform\"\n  }\n}\n\nresource \"aws_vpc\" \"main\" {\n  cidr_block = var.cidr_block\n  tags       = local.common_tags\n}",
			Exercise:       "Add a locals block with common_tags and use it on your VPC and instance. Validate locally.",
			ExpectedOutput: "Validation passes; both resources reference local.common_tags; the tag values appear in the plan preview attributes.",
			Mistakes:       []string{"Putting secrets in locals — locals are visible in plan output just like variables.", "Using locals for values the caller must set — that is a variable, not a local."},
			Quiz:           Quiz{Question: "When should a value be a local instead of a variable?", Options: []string{"When the caller must provide it", "When it is computed internally from other values and only used inside the module", "When it must be secret", "Always"}, Correct: 1, Explanation: "Locals are internal computed values; variables are caller-provided inputs."},
			XP:             10,
		},
		{
			ID: "l2-08-functions", Level: 2, Title: "Built-in Functions",
			Summary:        "The function library: string, collection, numeric, CIDR, and encoding functions.",
			Explanation:    "Terraform ships functions for the jobs expressions do repeatedly: format and string interpolation, merge/concat/keys/values for collections, max/min/ceil for numbers, cidrhost/cidrsubnet/cidrnetmask for networks, jsonencode/jsondecode for policy documents, try/can for fallbacks, and sensitive-aware handling. Knowing the catalogue matters less than knowing the patterns: lookup with a default for optional map keys, merge for combining tag maps, cidrsubnet for carving subnets from a VPC block, jsonencode for IAM policies.",
			UseCase:        "Subnets are carved from the VPC CIDR with cidrsubnet(var.cidr_block, 8, count.index) instead of hardcoding four CIDR blocks that someone must keep in sync.",
			CodeExample:    "locals {\n  public_cidr = cidrsubnet(var.cidr_block, 8, 0)\n  private_cidr = cidrsubnet(var.cidr_block, 8, 1)\n  tags = merge(local.common_tags, { Tier = \"public\" })\n}",
			Exercise:       "Replace a hardcoded subnet CIDR with cidrsubnet(var.cidr_block, 8, 0). What CIDR does it produce for a /16 VPC?",
			ExpectedOutput: "For 10.0.0.0/16, cidrsubnet(..., 8, 0) gives 10.0.0.0/24 and index 1 gives 10.0.1.0/24.",
			Mistakes:       []string{"Hardcoding CIDRs that cidrsubnet could derive — the hardcoded set drifts from the VPC block.", "Using jsonencode for a policy and then hand-editing the JSON — edit the expression, not the output."},
			Quiz:           Quiz{Question: "What does cidrsubnet(\"10.0.0.0/16\", 8, 1) return?", Options: []string{"10.0.0.0/16", "10.0.1.0/24", "10.1.0.0/24", "10.0.0.0/8"}, Correct: 1, Explanation: "It carves a /24 (16+8) out of the /16; index 1 is the second /24, 10.0.1.0/24."},
			XP:             10,
		},
		{
			ID: "l2-09-dynamic-blocks", Level: 2, Title: "Dynamic Blocks",
			Summary:        "Generate repeated nested blocks from a collection instead of copying them.",
			Explanation:    "Some arguments are nested blocks (ingress rules in a security group, tags, lifecycle rules). A dynamic block generates one nested block per element of a collection, with an iterator (each block's content uses ingress.value or a named iterator). Dynamic blocks keep the configuration DRY without changing the resource's shape — the plan shows the same blocks as if they were written out by hand.",
			UseCase:        "A security group opens ports 22, 80, and 443. A dynamic ingress block iterates over a list of ports instead of three copied blocks.",
			CodeExample:    "variable \"open_ports\" {\n  type    = list(number)\n  default = [22, 80, 443]\n}\n\nresource \"aws_security_group\" \"web\" {\n  vpc_id = aws_vpc.main.id\n\n  dynamic \"ingress\" {\n    for_each = var.open_ports\n    content {\n      from_port   = ingress.value\n      to_port     = ingress.value\n      protocol    = \"tcp\"\n      cidr_blocks = [\"0.0.0.0/0\"]\n    }\n  }\n}",
			Exercise:       "Add a security group to your project with a dynamic ingress block over ports [22, 80, 443]. Validate locally.",
			ExpectedOutput: "Validation passes; the security group declares three ingress rules from one dynamic block.",
			Mistakes:       []string{"Using dynamic blocks for top-level resources — dynamic is only for nested blocks; resources use for_each.", "Forgetting the iterator name — inside content, the value is <block>.value (or the named iterator), not var."},
			Quiz:           Quiz{Question: "What does a dynamic block generate?", Options: []string{"New resources", "Repeated nested blocks from a collection", "New providers", "New state files"}, Correct: 1, Explanation: "Dynamic blocks generate repeated nested blocks (like ingress rules) from a collection."},
			XP:             10,
		},
		{
			ID: "l2-10-lifecycle", Level: 2, Title: "Lifecycle Meta-arguments",
			Summary:        "create_before_destroy, prevent_destroy, ignore_changes: the lifecycle block controls replacement behaviour.",
			Explanation:    "The lifecycle block tunes how Terraform manages a resource's life: create_before_destroy (create the replacement before destroying the old one — zero-downtime replacements), prevent_destroy (refuse any plan that would destroy the resource — a guard for databases and buckets), ignore_changes (ignore specific attributes that drift by design, e.g. tags managed by another tool). Each is a sharp tool: prevent_destroy blocks legitimate destroys too, and ignore_changes hides real drift — use both deliberately and document why.",
			UseCase:        "A production database gets prevent_destroy: no plan — however accidental — can drop it. Removing the guard is itself a reviewed change.",
			CodeExample:    "resource \"aws_db_instance\" \"main\" {\n  allocated_storage = 20\n  engine            = \"postgres\"\n\n  lifecycle {\n    prevent_destroy = true\n  }\n}",
			Exercise:       "Add prevent_destroy to your S3 bucket resource (if your project has one) and run a plan preview after removing the bucket from configuration. What does the preview say?",
			ExpectedOutput: "The preview (a real plan) refuses: the destroy is blocked by prevent_destroy. The local preview lists the delete; the lifecycle rule is enforced at plan/apply time by the binary, not by the local preview — note the difference.",
			Mistakes:       []string{"ignore_changes on an attribute you actually manage — drift becomes invisible.", "create_before_destroy on a resource with a unique name — the replacement cannot be created while the old one holds the name."},
			Quiz:           Quiz{Question: "What does prevent_destroy do?", Options: []string{"Deletes the resource faster", "Makes any plan that would destroy the resource fail", "Ignores all changes", "Creates a backup"}, Correct: 1, Explanation: "prevent_destroy refuses plans that would destroy the resource — a guard for stateful resources."},
			XP:             10,
		},
		{
			ID: "l2-11-depends-on", Level: 2, Title: "depends_on and Explicit Dependencies",
			Summary:        "When references are not enough: explicit dependencies for ordering without data flow.",
			Explanation:    "Most dependencies are implicit (references). depends_on declares an explicit dependency when ordering matters but no attribute is referenced — for example, an instance that must start only after a policy is attached, or a resource that depends on a side effect. It is a blunt instrument: overuse creates a brittle, over-serialised graph. Prefer references; reach for depends_on only when there is genuinely no reference to express the ordering.",
			UseCase:        "A Lambda function's IAM role needs a policy attachment to exist before the function is created, but the function never references the attachment's attributes — depends_on expresses the ordering.",
			CodeExample:    "resource \"aws_lambda_function\" \"hello\" {\n  function_name = \"hello\"\n  runtime       = \"python3.9\"\n  handler       = \"index.handler\"\n\n  depends_on = [aws_iam_role_policy_attachment.app]\n}",
			Exercise:       "Find a place in your project where two resources have an ordering need but no reference. Add depends_on and explain in a comment why a reference cannot express it — or conclude there is no such place and say why.",
			ExpectedOutput: "Either a depends_on with a justifying comment, or a written conclusion that all ordering in the project is already expressed by references (the common, healthy case).",
			Mistakes:       []string{"Using depends_on to fix a cycle — it usually makes the cycle explicit, not fixed.", "depends_on on whole modules when only one resource needs ordering — it serialises everything."},
			Quiz:           Quiz{Question: "When should you use depends_on?", Options: []string{"Always, for safety", "Only when ordering matters but no attribute reference expresses it", "To make plans faster", "Never — it is deprecated"}, Correct: 1, Explanation: "depends_on is for ordering without data flow; references should express dependencies whenever possible."},
			XP:             10,
		},
		{
			ID: "l2-12-provisioners", Level: 2, Title: "Provisioners — and Why to Avoid Them",
			Summary:        "Provisioners run scripts on creation; they are a last resort, not a deployment strategy.",
			Explanation:    "Provisioners (local-exec, remote-exec, file) run commands when a resource is created or destroyed. They exist for bootstrapping that nothing else can do — but they are a last resort: they run only at create/destroy time (not on update), they fail the whole apply when they fail, they cannot be planned, and they leave no record in state of what they did. The honest alternatives: user_data / cloud-init for instances, configuration management for servers, and CI pipelines for deployments. If you must use one, prefer local-exec with a script that is idempotent and logged.",
			UseCase:        "An instance needs a one-time bootstrap that cloud-init cannot express. A remote-exec provisioner runs it — once, at creation, with the understanding that a rebuild re-runs it from scratch.",
			CodeExample:    "# Last resort only — prefer user_data / cloud-init.\nresource \"aws_instance\" \"web\" {\n  ami           = data.aws_ami.latest.id\n  instance_type = \"t3.micro\"\n\n  provisioner \"local-exec\" {\n    command = \"echo ${self.private_ip} >> inventory.txt\"\n  }\n}",
			Exercise:       "Write down one task in your project that a provisioner could do and the non-provisioner way to do it instead (user_data, CI step, or configuration management). Which is more repeatable, and why?",
			ExpectedOutput: "A named task with both approaches and a reason the non-provisioner one is more repeatable (runs on every rebuild, is versioned, leaves a log).",
			Mistakes:       []string{"Using provisioners as a deployment pipeline — deploys happen after creation, provisioners do not.", "A provisioner that fails silently (|| true) — the apply reports success for work that never happened."},
			Quiz:           Quiz{Question: "Why are provisioners a last resort?", Options: []string{"They are slow", "They run only at create/destroy, cannot be planned, and leave no record of what they did", "They cost extra", "They need a licence"}, Correct: 1, Explanation: "Provisioners are unplanned, create/destroy-only side effects with no state record — the alternatives are repeatable and auditable."},
			XP:             10,
		},
		{
			ID: "l2-13-import", Level: 2, Title: "Importing Existing Resources",
			Summary:        "Bring infrastructure created outside Terraform under management without recreating it.",
			Explanation:    "terraform import (or the import block, the modern form) records an existing real resource into state under a resource address. Import writes state only — it does not generate configuration. The workflow is: write the resource block to match reality, import, then plan: the plan should show no changes (or only the attributes you intend to fix). A plan that wants to replace the resource after import means the configuration does not match reality yet. Import is how teams adopt Terraform without downtime.",
			UseCase:        "A VPC was created by hand in the console years ago. The team writes its resource block, imports it, and from then on the VPC is managed — never recreated.",
			CodeExample:    "# import block (Terraform >= 1.5):\nimport {\n  to = aws_vpc.main\n  id = \"vpc-0123456789abcdef0\"\n}\n\nresource \"aws_vpc\" \"main\" {\n  cidr_block = \"10.0.0.0/16\"\n}",
			Exercise:       "Describe the import workflow for a bucket created in the CloudForge S3 console: what resource block would you write, what would you import it as, and what should the first plan after import show?",
			ExpectedOutput: "A resource block matching the bucket, an import of its id, and a first plan showing no changes — proof the configuration matches reality.",
			Mistakes:       []string{"Importing before writing the configuration — Terraform has nothing to attach the state to.", "Assuming import generates configuration — it only writes state; the block must already exist and match."},
			Quiz:           Quiz{Question: "What does terraform import do?", Options: []string{"Creates the resource", "Records an existing real resource into state under a resource address", "Generates configuration from the cloud", "Deletes the resource"}, Correct: 1, Explanation: "Import writes state only. Configuration must already exist and match, or the next plan will show changes."},
			XP:             10,
		},
		{
			ID: "l2-14-moved-blocks", Level: 2, Title: "Moved Blocks and Refactoring",
			Summary:        "Rename and restructure without destroying: moved blocks tell Terraform an address changed, not the resource.",
			Explanation:    "Refactoring changes addresses: a resource moves into a module, or a single block becomes a for_each. Without help, Terraform reads the new address as a new resource and the old address as a deletion — destroy and recreate. A moved block (moved { from = aws_subnet.old to = aws_subnet.new } or module moves) tells Terraform the resource is the same one at a new address, so the plan shows no change. The state mv command does the same thing imperatively; moved blocks are the declarative, reviewable form — and they can be removed once the state has caught up.",
			UseCase:        "Two subnet blocks become one for_each. A moved block per subnet maps the old addresses to the new for_each addresses; the plan shows no changes and nothing is recreated.",
			CodeExample:    "moved {\n  from = aws_subnet.public\n  to   = aws_subnet.this[\"cf-local-1a\"]\n}\n\nmoved {\n  from = aws_subnet.private\n  to   = aws_subnet.this[\"cf-local-1b\"]\n}",
			Exercise:       "You converted two subnet blocks to for_each in lesson 2-05. Write the two moved blocks that would make that refactor a no-op in a real plan, and explain what the plan would show without them.",
			ExpectedOutput: "Two moved blocks mapping old addresses to for_each addresses; without them the plan shows destroy+create for both subnets.",
			Mistakes:       []string{"Refactoring addresses without moved blocks and accepting the destroy+create as 'normal' — it is avoidable data-plane churn.", "Leaving moved blocks in forever — remove them after the state has moved; they are migration scaffolding."},
			Quiz:           Quiz{Question: "What does a moved block do?", Options: []string{"Moves a resource to another region", "Tells Terraform a resource has a new address but is the same real resource", "Renames the resource in the cloud", "Deletes the old resource"}, Correct: 1, Explanation: "moved blocks map old addresses to new ones so refactoring does not destroy and recreate."},
			XP:             10,
		},
		{
			ID: "l2-15-taint-replace", Level: 2, Title: "Taint and Replace",
			Summary:        "When a resource is broken but Terraform thinks it is fine: taint, replace, and -replace.",
			Explanation:    "Sometimes a resource is degraded in a way state cannot see (a corrupted instance, a bad disk). terraform taint marks it for replacement on the next apply; -replace targets one address for replacement in a single run. The modern form is the replace trigger: terraform apply -replace=aws_instance.web. Replacement destroys the old resource and creates a new one — for stateful resources that is data loss, so the plan must be read first. Taint is per-resource and deliberate; never taint a database casually.",
			UseCase:        "An instance's disk is corrupted but its state says 'running'. -replace recreates exactly that instance; everything else is untouched.",
			CodeExample:    "$ terraform apply -replace=aws_instance.web\n\n# aws_instance.web must be replaced\n-/+ resource \"aws_instance\" \"web\" {\n      ~ id = \"i-0123\" -> (known after apply)\n    }\n\nPlan: 1 to add, 0 to change, 1 to destroy.",
			Exercise:       "Your instance is 'running' in state but unreachable. Write the command that replaces only it, and describe what the plan would show before you confirm.",
			ExpectedOutput: "terraform apply -replace=<address>; the plan shows -/+ for that instance only (1 to add, 1 to destroy) and no-op for everything else.",
			Mistakes:       []string{"Tainting a resource to 'fix' a configuration error — fix the configuration; taint only replaces the real object.", "Replacing a stateful resource without a backup — replacement destroys the old object and its data."},
			Quiz:           Quiz{Question: "What does -replace do?", Options: []string{"Deletes state", "Replaces one resource (destroy + create) in the next apply", "Renames a resource", "Ignores the resource"}, Correct: 1, Explanation: "-replace marks one address for destroy-and-create; everything else is untouched."},
			XP:             10,
		},
		{
			ID: "l2-16-variable-validation", Level: 2, Title: "Variable Validation",
			Summary:        "Validation blocks catch bad inputs at plan time, with messages a human can act on.",
			Explanation:    "A validation block inside a variable checks its value with a condition and reports a clear error_message when it fails. Validation runs at plan time — before anything is created — and it is the cheapest place to catch a wrong CIDR, an unknown environment, or a negative count. Validation can reference only the variable itself (not other variables or resources), so cross-variable rules belong in preconditions (Level 3) or in the module's documentation.",
			UseCase:        "A CIDR variable with a validation that it parses as a CIDR and is at least a /16: a typo like '10.0.0.0/33' fails the plan in seconds instead of failing the VPC creation.",
			CodeExample:    "variable \"cidr_block\" {\n  type = string\n\n  validation {\n    condition     = can(cidrhost(var.cidr_block, 0))\n    error_message = \"cidr_block must be a valid CIDR block.\"\n  }\n}",
			Exercise:       "Add a validation block to your environment variable: it must be one of dev, staging, prod. Test it by setting an invalid value in terraform.tfvars and running local validate — then fix it.",
			ExpectedOutput: "With the invalid value, validation fails with your error message. (The local validator checks references and structure; the binary enforces validation blocks at plan time — note which layer catches it.)",
			Mistakes:       []string{"An error_message that says 'invalid value' without saying what is valid — the message is the documentation.", "Validating with a condition that references another variable — validation blocks can only see their own variable."},
			Quiz:           Quiz{Question: "When does variable validation run?", Options: []string{"At apply only", "At plan time, before anything is created", "At init", "Never — it is documentation"}, Correct: 1, Explanation: "Validation blocks are checked at plan time, so bad inputs fail fast."},
			XP:             10,
		},
		{
			ID: "l2-17-sensitive-values", Level: 2, Title: "Sensitive Values",
			Summary:        "Sensitive marks hide values from output — they do not encrypt them in state.",
			Explanation:    "sensitive = true on a variable or output hides the value from CLI output (shown as <sensitive>), and Terraform tracks sensitivity through expressions so a derived value is hidden too. What it does NOT do: encrypt the value in state. State files contain sensitive values in plain text — which is why state backends must be encrypted and access-controlled, and why real secrets belong in a secrets manager (or the CloudForge IAM teaching store), referenced — never stored — in configuration.",
			UseCase:        "A database password is passed as a sensitive variable: it never appears in plan logs or CI output, and the state backend is encrypted and restricted to the team.",
			CodeExample:    "variable \"db_password\" {\n  type      = string\n  sensitive = true\n}\n\noutput \"db_password\" {\n  value     = var.db_password\n  sensitive = true\n}",
			Exercise:       "Mark one variable in your project sensitive and run a plan preview. How is its value displayed? Then check: where would the value still be visible (state file)?",
			ExpectedOutput: "The preview/plan shows <sensitive> instead of the value. The value is still in the state file in plain text — the lesson's point.",
			Mistakes:       []string{"Believing sensitive = encrypted — it is display-only; state security is a backend concern.", "Committing a tfvars file with a 'sensitive' password because the variable is marked sensitive — the file is still plain text in git."},
			Quiz:           Quiz{Question: "What does sensitive = true guarantee?", Options: []string{"The value is encrypted in state", "The value is hidden from CLI output", "The value cannot be read by anyone", "The value is rotated"}, Correct: 1, Explanation: "Sensitive is display-only. State still holds the value in plain text; backend encryption and access control protect it."},
			XP:             10,
		},
		{
			ID: "l2-18-tfvars-precedence", Level: 2, Title: "tfvars and Variable Precedence",
			Summary:        "Where values come from, in order: defaults, environment, tfvars files, -var flags.",
			Explanation:    "A variable's value is resolved in a fixed precedence order (lowest to highest): default in the variable block, environment variable TF_VAR_name, terraform.tfvars (and *.auto.tfvars, loaded automatically), -var-file flags, and finally -var flags. *.auto.tfvars files load without being named; terraform.tfvars is the conventional per-directory values file. Environment-specific values belong in per-environment tfvars (dev.tfvars, prod.tfvars) selected with -var-file — never in the committed default tfvars with secrets in it.",
			UseCase:        "CI applies prod with -var-file=prod.tfvars: the file is reviewed, versioned, and selected explicitly; devs run locally with terraform.tfvars defaults.",
			CodeExample:    "# precedence, lowest to highest:\n# 1. variable default\n# 2. TF_VAR_cidr_block environment variable\n# 3. terraform.tfvars / *.auto.tfvars\n# 4. -var-file=prod.tfvars\n# 5. -var 'cidr_block=10.9.0.0/16'\n\n$ terraform plan -var-file=prod.tfvars",
			Exercise:       "Set your instance_type in three places at once (default, terraform.tfvars, and a -var flag in your head). Which one wins? Confirm by reasoning through the precedence list — and note that the local plan preview uses the tfvars value.",
			ExpectedOutput: "The -var flag wins, then -var-file, then terraform.tfvars/auto.tfvars, then the environment variable, then the default.",
			Mistakes:       []string{"A stray *.auto.tfvars in a subdirectory silently overriding values — auto-loading is convenient and invisible.", "Two tfvars files setting the same variable and assuming the first wins — the last -var-file wins."},
			Quiz:           Quiz{Question: "Which has the highest precedence for a variable value?", Options: []string{"The variable default", "terraform.tfvars", "A -var flag on the command line", "TF_VAR_ environment variable"}, Correct: 2, Explanation: "-var flags beat -var-file, which beats tfvars/auto.tfvars, which beats environment variables, which beat defaults."},
			XP:             10,
		},
		{
			ID: "l2-19-outputs-remote-state", Level: 2, Title: "Outputs and Remote State References",
			Summary:        "One stack reads another's outputs through remote state — the contract between stacks.",
			Explanation:    "Stacks are split by blast radius (networking, compute, data), and they share values through remote state: terraform_remote_state reads another stack's state file and exposes its outputs as data. The networking stack outputs vpc_id; the compute stack reads it. The contract is the outputs — rename one and the consumer breaks at plan time, which is the point: the break is loud and early. Remote state references create coupling, so keep them few and stable.",
			UseCase:        "The network stack owns the VPC. The application stack reads its vpc_id and subnet IDs from remote state instead of duplicating the CIDR math.",
			CodeExample:    "data \"terraform_remote_state\" \"network\" {\n  backend = \"s3\"\n  config = {\n    bucket = \"my-tf-state\"\n    key    = \"network/terraform.tfstate\"\n    region = \"us-east-1\"\n  }\n}\n\nresource \"aws_instance\" \"web\" {\n  subnet_id = data.terraform_remote_state.network.outputs.public_subnet_id\n}",
			Exercise:       "Design a two-stack split of your project: what lives in the network stack, what lives in the app stack, and which three outputs does the network stack expose? Write the output blocks.",
			ExpectedOutput: "Network stack: VPC, subnets, gateways, security groups; outputs vpc_id, public_subnet_id, private_subnet_id. App stack: instances, buckets; inputs are the three outputs.",
			Mistakes:       []string{"Reading another stack's whole state instead of its outputs — outputs are the contract; internals change.", "Circular remote-state references (A reads B, B reads A) — stacks must form a DAG."},
			Quiz:           Quiz{Question: "How does one stack read another stack's values?", Options: []string{"By copying the files", "Through terraform_remote_state, reading the other stack's outputs", "Through a shared variable file", "It cannot"}, Correct: 1, Explanation: "terraform_remote_state exposes another stack's outputs as data; outputs are the inter-stack contract."},
			XP:             10,
		},
		{
			ID: "l2-20-version-constraints", Level: 2, Title: "Version Constraints",
			Summary:        "required_version and provider version constraints: upgrades as reviewed decisions.",
			Explanation:    "Version constraints declare which versions are acceptable: required_version = \">= 1.5\" for Terraform itself, and version = \"~> 5.0\" for a provider (the pessimistic operator: >= 5.0, < 6.0). The lock file (.terraform.lock.hcl) records the exact versions installed, and should be committed — it makes init reproducible. Upgrading is a deliberate act: change the constraint, run init -upgrade, read the plan, and review the changelog. Unconstrained versions are how a routine init silently becomes a breaking upgrade.",
			UseCase:        "A provider releases 6.0 with breaking changes. The ~> 5.0 constraint keeps every init on 5.x until the team reviews 6.0 and upgrades deliberately.",
			CodeExample:    "terraform {\n  required_version = \">= 1.5\"\n\n  required_providers {\n    aws = {\n      source  = \"hashicorp/aws\"\n      version = \"~> 5.0\"\n    }\n  }\n}",
			Exercise:       "Add versions.tf to your project with required_version >= 1.5 and the AWS provider ~> 5.0. Validate locally, then explain what the lock file would contain after init.",
			ExpectedOutput: "versions.tf with both constraints; validation green. The lock file would record the exact provider version and hashes installed by init.",
			Mistakes:       []string{"No constraints at all — the next init can install a major version.", "Committing .terraform but not the lock file — the cache is regenerable, the lock file is the reproducibility record."},
			Quiz:           Quiz{Question: "What does version = \"~> 5.0\" allow?", Options: []string{"Any version", "5.x versions (>= 5.0, < 6.0)", "Only exactly 5.0", "6.0 and above"}, Correct: 1, Explanation: "~> 5.0 is the pessimistic operator: it allows 5.x patch/minor updates but not 6.0."},
			XP:             10,
		},
	},
}

var level3 = Level{
	Level: 3, Title: "Advanced",
	Description: "State surgery, testing, policy, pipelines, drift, and team workflows at scale.",
	Lessons: []Lesson{
		{
			ID: "l3-01-remote-state-locking", Level: 3, Title: "Remote State and Locking Deep Dive",
			Summary:        "How locking actually works, what happens when it fails, and how to recover safely.",
			Explanation:    "A backend lock is a record (a DynamoDB item for S3 backends; a lock row in the CloudForge learning store) with an owner, a timestamp, and an expiry. Terraform takes the lock before plan/apply and releases it after. If a process dies mid-apply, the lock stays: the next run fails with a lock error naming the holder. The recovery is deliberate — verify nobody is applying, then force-unlock that specific lock ID (never delete the lock table). Locking does not make applies atomic: a crashed apply still leaves partial state, which the next plan reconciles.",
			UseCase:        "CI is killed mid-apply. The next run reports the state is locked by the dead run. The operator verifies no apply is running, force-unlocks that lock ID, and re-plans.",
			CodeExample:    "$ terraform plan\nError: Error acquiring the state lock\n  Lock Info:\n    ID:        3f2a1b\n    Path:      my-tf-state/network/terraform.tfstate\n    Operation: OperationTypeApply\n    Who:       ci-runner-4\n\n$ terraform force-unlock 3f2a1b",
			Exercise:       "Lock your project state, start a plan preview (it should be refused), then force-unlock via the State tab using the lock ID shown. What does the audit trail record?",
			ExpectedOutput: "Plan preview refused with 409 state_locked. After force-unlock with the shown lock ID, the preview succeeds; the lock/unlock are recorded in the run history.",
			Mistakes:       []string{"Force-unlocking while an apply is actually running — two writers corrupt state.", "Deleting the lock record by hand in the backend — use force-unlock so the operation is logged."},
			Quiz:           Quiz{Question: "A lock error names a lock ID. What should you do first?", Options: []string{"Delete the state file", "Verify nobody is actually applying, then force-unlock that specific lock ID", "Ignore it and apply anyway", "Re-run init"}, Correct: 1, Explanation: "Confirm no apply is running first; then force-unlock the named lock. Never unlock blindly."},
			XP:             15,
		},
		{
			ID: "l3-02-state-surgery", Level: 3, Title: "State Surgery (Safe)",
			Summary:        "mv, rm, import, and push/pull — the state commands, used only through versioned snapshots.",
			Explanation:    "State surgery is any direct manipulation of state: terraform state mv (rename an address), state rm (stop managing a resource without destroying it — the real object keeps running, unmanaged), state push/pull (replace or fetch state), and import. These are sharp tools: every one of them can orphan resources or lose the record. The CloudForge rule is absolute: state is append-only versioned snapshots; surgery happens by creating a new version (backup first, restore by copying an old version forward), never by editing a state document in place with string replacement.",
			UseCase:        "A resource was renamed in configuration. state mv renames its address in state so the next plan is a no-op instead of destroy+create.",
			CodeExample:    "$ terraform state mv aws_subnet.old aws_subnet.new\nMove \"aws_subnet.old\" to \"aws_subnet.new\"\nSuccessfully moved 1 object(s).\n\n# In CloudForge: State tab -> Backup creates a versioned\n# snapshot first; Restore copies a version forward as a\n# NEW version — history is never rewritten.",
			Exercise:       "Back up your project's state from the State tab, then restore the backup. How many versions exist afterwards, and which one is current?",
			ExpectedOutput: "One new version is created by the backup and one by the restore; the restored content is current, and the previous version is still in history.",
			Mistakes:       []string{"Editing a state file in a text editor — one bad character corrupts the record of everything.", "state rm on a resource you meant to destroy — rm only stops managing it; the real object keeps running (and billing)."},
			Quiz:           Quiz{Question: "What does terraform state rm do?", Options: []string{"Destroys the real resource", "Removes the resource from state so Terraform stops managing it; the real object keeps running", "Deletes the state file", "Renames the resource"}, Correct: 1, Explanation: "rm only forgets the resource. The real object continues to exist — unmanaged and billable."},
			XP:             15,
		},
		{
			ID: "l3-03-workspaces-vs-dirs", Level: 3, Title: "Workspaces vs Directories vs Stacks",
			Summary:        "Three ways to separate environments, and how to choose honestly.",
			Explanation:    "Workspaces share code and backend with separate state files — good for near-identical environments. Separate directories (live/dev, live/prod) duplicate the code layout but let each environment diverge and be versioned independently — good when environments genuinely differ. Separate stacks (networking, app, data) split by blast radius with remote-state references between them. The failure mode of each: workspaces collapse under conditionals, directories drift apart silently, stacks create coupling. Choose by how different the environments really are, not by fashion.",
			UseCase:        "Dev and prod started as workspaces. Prod grew a NAT gateway, extra AZs, and stricter backups; the workspace code is now 40% conditionals. The team splits into directories.",
			CodeExample:    "live/\n  dev/   # same modules, dev.tfvars\n  prod/  # same modules, prod.tfvars\nmodules/\n  network/\n  compute/",
			Exercise:       "Score your project on three axes: how different would dev and prod be (1-5), how many stacks share values (1-5), how often environments change independently (1-5). Which separation fits, and why?",
			ExpectedOutput: "A reasoned choice (workspaces for near-identical, directories for divergent, stacks for shared values) tied to your scores.",
			Mistakes:       []string{"One workspace per customer at 200 customers — workspaces do not scale to multi-tenancy.", "Splitting stacks before there is a real blast-radius reason — coupling without benefit."},
			Quiz:           Quiz{Question: "When do workspaces break down?", Options: []string{"When there are more than two", "When environments genuinely diverge and the code fills with conditionals", "When state is remote", "Never"}, Correct: 1, Explanation: "Workspaces assume near-identical environments; divergence turns the shared code into conditionals."},
			XP:             15,
		},
		{
			ID: "l3-04-module-composition", Level: 3, Title: "Module Composition Patterns",
			Summary:        "Composing small modules into systems: flat, layered, and facade patterns.",
			Explanation:    "Small modules compose into systems in a few proven shapes: flat (root calls network, compute, and data modules side by side), layered (network outputs feed compute inputs, compute outputs feed app inputs), and facade (one root module wraps several submodules behind a single interface for consumers). Composition rules: modules depend on outputs, never on internals; shared values (tags, names, environment) flow down as variables; a module never reads another module's state directly. Deep nesting (modules calling modules calling modules) hides the graph — keep the tree shallow.",
			UseCase:        "The platform team ships a facade module 'web-service' (network + compute + load balancer) that product teams call with three variables; the internals change without breaking callers.",
			CodeExample:    "module \"network\" { source = \"./modules/network\" ... }\n\nmodule \"compute\" {\n  source    = \"./modules/compute\"\n  vpc_id    = module.network.vpc_id\n  subnet_id = module.network.public_subnet_id\n}",
			Exercise:       "Sketch your project's module tree (root → modules → resources). Mark every cross-module edge: is it an output/input pair, or does something reach into another module's internals?",
			ExpectedOutput: "A tree with only output/input edges; any internal reach is flagged as a composition smell to fix.",
			Mistakes:       []string{"Modules reaching into each other's state or internals — the contract is outputs.", "A facade that hides so much that callers cannot debug it — expose the outputs that matter."},
			Quiz:           Quiz{Question: "What should modules depend on?", Options: []string{"Each other's internal resources", "Each other's outputs, passed as inputs", "Shared state files", "Global variables"}, Correct: 1, Explanation: "Modules compose through outputs-as-inputs; internals stay hidden behind the contract."},
			XP:             15,
		},
		{
			ID: "l3-05-testing", Level: 3, Title: "Testing Terraform",
			Summary:        "terraform test, plan assertions, and the testing pyramid for infrastructure.",
			Explanation:    "Terraform has a native test framework (terraform test): .tftest.hcl files with run blocks that apply configuration (often with mocked providers) and assert on outputs and resource attributes. Below that: validate and fmt in CI (fast, always), plan assertions in pipelines (does the plan contain exactly the expected changes?), and policy checks (Level 3-06). Above it: ephemeral-environment tests that actually apply and destroy. The pyramid rule applies: many fast static checks, some plan tests, few real applies.",
			UseCase:        "A network module's test asserts the VPC CIDR, the subnet count, and that no subnet is public by default — the test runs in CI on every change to the module.",
			CodeExample:    "# tests/network.tftest.hcl\nrun \"vpc_cidr\" {\n  command = plan\n\n  assert {\n    condition     = aws_vpc.main.cidr_block == \"10.0.0.0/16\"\n    error_message = \"VPC CIDR must be 10.0.0.0/16\"\n  }\n}",
			Exercise:       "Write three plan assertions for your project as plain sentences (e.g. 'the VPC CIDR is 10.0.0.0/16', 'there are exactly two subnets', 'no security group allows 0.0.0.0/0 on port 22'). Which layer would check each (validate, plan test, or real apply)?",
			ExpectedOutput: "Three assertions, each assigned to a layer: static checks for syntax, plan tests for the diff, real applies only for behaviour.",
			Mistakes:       []string{"Testing only by applying to prod — the most expensive test environment there is.", "Assertions that restate the configuration instead of the intent (asserting the CIDR equals the variable is tautological; assert it is a /16 in the right range)."},
			Quiz:           Quiz{Question: "What does terraform test assert on?", Options: []string{"Only syntax", "Plan/apply results: outputs and resource attributes after a run", "Provider download speed", "State file size"}, Correct: 1, Explanation: "terraform test runs blocks and asserts on outputs and attributes — real behaviour, not just syntax."},
			XP:             15,
		},
		{
			ID: "l3-06-policy-as-code", Level: 3, Title: "Policy as Code",
			Summary:        "Guardrails as tests: Sentinel/OPA-style rules that reject dangerous plans automatically.",
			Explanation:    "Policy as code turns team rules into automated checks on the plan: 'no security group may allow 0.0.0.0/0 to port 22', 'every resource must have a CostCenter tag', 'no database may be publicly accessible'. Tools (Sentinel, OPA/Conftest) evaluate the plan JSON — the same parsed plan Terraform produces — and fail the pipeline when a rule is violated. Policies are code: versioned, reviewed, and tested. The CloudForge learning equivalent is the plan preview plus the local validator's rules (forbidden hosts, embedded credentials) — the same idea at teaching scale.",
			UseCase:        "A policy forbids public S3 buckets. A pull request's plan creates one; the policy check fails the pipeline before any human reviews it.",
			CodeExample:    "# Policy (conceptual — OPA/Rego style):\n# deny if any aws_security_group ingress allows 0.0.0.0/0 to port 22\n#\n# deny[msg] {\n#   some r in input.resource_changes\n#   r.type == \"aws_security_group\"\n#   some rule in r.change.after.ingress\n#   rule.from_port == 22\n#   \"0.0.0.0/0\" in rule.cidr_blocks\n#   msg := \"SSH must not be open to the world\"\n# }",
			Exercise:       "Write three policies for your project as plain-language rules (e.g. 'no 0.0.0.0/0 ingress on port 22'). For each, say whether the local validator, the plan preview, or a human reviewer would catch a violation today.",
			ExpectedOutput: "Three rules with an honest catcher for each — and at least one that only a human or a real policy engine would catch (a gap to note, not to hide).",
			Mistakes:       []string{"Policies that duplicate what the plan already shows — policy should check intent (no public SSH), not syntax.", "A policy nobody can override with a reviewed exception — teams route around absolute blocks."},
			Quiz:           Quiz{Question: "What does policy as code check?", Options: []string{"Code formatting", "The plan against team rules (e.g. no public SSH) before apply", "Provider versions", "State file syntax"}, Correct: 1, Explanation: "Policy engines evaluate the plan JSON against rules and fail the pipeline on violations."},
			XP:             15,
		},
		{
			ID: "l3-07-cicd", Level: 3, Title: "Terraform in CI/CD",
			Summary:        "The pipeline shape: fmt, validate, plan on PRs; approve, apply on merge — with the plan as the review artefact.",
			Explanation:    "The standard pipeline: on a pull request, run fmt -check, validate, and plan, and post the plan as a comment — the plan IS the change request. On merge, a human approves and the pipeline applies the exact plan that was reviewed (plan file, not a fresh plan). State locking prevents concurrent applies; the apply log is the audit trail. Secrets are injected by the CI system, never committed. CloudForge mirrors this in miniature: plan preview for review, explicit approval with a 1-hour TTL, confirmation-gated apply, and an execution history with real logs.",
			UseCase:        "A developer opens a PR adding a subnet. CI posts the plan; a reviewer sees '1 to add' and approves. On merge, CI applies the reviewed plan file.",
			CodeExample:    "# Pipeline stages:\n# PR:  fmt -check -> validate -> plan -> post plan as comment\n# Merge: approve (human) -> apply plan.tfplan -> verify outputs\n# Never: plan again at apply time (the plan must be the reviewed one).",
			Exercise:       "Map the CloudForge execution flow to the pipeline above: which step is the plan preview, which is approval, which is apply, and where does the 1-hour plan TTL fit?",
			ExpectedOutput: "Preview = PR plan; approve endpoint = human approval; apply with plan_id + confirm = merge apply; the TTL bounds how long an approval stays valid.",
			Mistakes:       []string{"Planning again at apply time — the applied plan may differ from the reviewed one.", "Applying from a developer laptop in a team — applies belong in CI, where they are logged and locked."},
			Quiz:           Quiz{Question: "Why apply the exact plan file that was reviewed?", Options: []string{"It is faster", "So the applied change is exactly the reviewed change, not a fresh plan that may differ", "It saves state", "It is required by law"}, Correct: 1, Explanation: "Re-planning at apply time can produce a different diff; the reviewed plan file is the contract."},
			XP:             15,
		},
		{
			ID: "l3-08-drift", Level: 3, Title: "Drift Detection and Remediation",
			Summary:        "Reality moves without Terraform: detect drift, decide whether to reconcile or adopt it.",
			Explanation:    "Drift is the gap between recorded state and the real world, caused by out-of-band changes (console edits, other tools, manual fixes). Detection: refresh during plan, or a dedicated drift check comparing state to live resources (the CloudForge State tab's drift view does exactly this against live lab resources). Remediation is a decision, not a reflex: if the change was a mistake, apply to reconcile; if it was intentional, update the configuration (and import if needed) to adopt it. Silent auto-reconciliation destroys intentional hotfixes.",
			UseCase:        "Someone opens port 22 to the world in the console during an incident. The next drift check flags the security group; the team decides the fix stays and updates the configuration to match — deliberately, in a reviewed change.",
			CodeExample:    "$ terraform plan -refresh-only\n# aws_security_group.web has changed (drift):\n# ingress rule for port 22 was added outside Terraform",
			Exercise:       "Use the State tab's drift check on your project. If it reports no drift, describe one change you could make outside Terraform (e.g. via the lab console) that would show up as drift, and what you would do about it.",
			ExpectedOutput: "A drift report (empty or with entries) plus a named out-of-band change and the reconcile-or-adopt decision for it.",
			Mistakes:       []string{"Auto-applying to 'fix' drift without reading it — drift sometimes encodes an intentional hotfix.", "Treating drift as a Terraform bug — drift is a process signal: something changed outside the workflow."},
			Quiz:           Quiz{Question: "What is drift?", Options: []string{"A plan error", "Manual changes outside Terraform that make reality differ from state", "A slow apply", "A module version change"}, Correct: 1, Explanation: "Drift is the gap between recorded state and the real world caused by out-of-band changes."},
			XP:             15,
		},
		{
			ID: "l3-09-refactoring", Level: 3, Title: "Refactoring at Scale",
			Summary:        "Large refactors without downtime: moved blocks, staged plans, and the blast-radius rule.",
			Explanation:    "Refactoring at scale follows three rules. One: change addresses with moved blocks, never by destroy+create (Level 2-14). Two: stage the change — one module at a time, plan after each, apply only when the plan is clean. Three: respect blast radius — refactor networking separately from compute, and never refactor stateful resources in the same change as the code that uses them. The plan is the safety net at every step: if a staged plan shows unexpected destroys, stop.",
			UseCase:        "A monolith configuration is split into network/compute/data modules over three reviewed PRs, each planned separately, with moved blocks carrying every address across.",
			CodeExample:    "# Refactor checklist:\n# 1. moved blocks for every renamed address\n# 2. plan: expect 0 to add, 0 to change, 0 to destroy\n# 3. apply the no-op plan (state catches up)\n# 4. remove the moved blocks in a follow-up PR\n# 5. repeat for the next module",
			Exercise:       "Plan a refactor of your project into two modules (network + compute) as a checklist: which addresses move, which moved blocks you need, and what the plan must show at each step.",
			ExpectedOutput: "A staged checklist with moved blocks listed and the expected plan (0/0/0) at each step.",
			Mistakes:       []string{"Big-bang refactors — one PR that moves everything and shows 40 destroys.", "Refactoring and changing behaviour in the same PR — the plan cannot tell which change caused a destroy."},
			Quiz:           Quiz{Question: "What must a pure refactor plan show?", Options: []string{"Many creates", "0 to add, 0 to change, 0 to destroy", "Many destroys", "An error"}, Correct: 1, Explanation: "A pure refactor only moves addresses; the plan must be a no-op, or something is being destroyed."},
			XP:             15,
		},
		{
			ID: "l3-10-multi-env", Level: 3, Title: "Multi-Account and Multi-Environment Patterns",
			Summary:        "One codebase, many accounts: provider aliases, assume-role, and per-environment backends.",
			Explanation:    "Real organisations run one AWS account per environment (or per team). Terraform handles this with provider aliases (multiple provider configurations in one run), assume-role (the CI role assumes the target account's deployment role), and per-environment backend keys (one state file per account/environment). The account boundary is a security boundary: a plan for dev can never touch prod state, because it cannot assume prod's role. In CloudForge the analogue is per-user project scoping and per-lab execution — the same separation idea at teaching scale.",
			UseCase:        "CI deploys to dev, staging, and prod accounts from one repository: three provider aliases, three backend keys, three assume-role chains — one reviewed plan per account.",
			CodeExample:    "provider \"aws\" {\n  alias  = \"prod\"\n  region = \"us-east-1\"\n  assume_role {\n    role_arn = \"arn:aws:iam::123456789012:role/TerraformDeploy\"\n  }\n}\n\nresource \"aws_vpc\" \"prod\" {\n  provider   = aws.prod\n  cidr_block = \"10.1.0.0/16\"\n}",
			Exercise:       "Design the account layout for a three-environment team: how many accounts, which role deploys to each, and what stops a dev plan from touching prod state?",
			ExpectedOutput: "Three accounts (or a justified alternative), a deployment role per account, and the mechanism that blocks cross-account applies (assume-role + separate backends).",
			Mistakes:       []string{"One account for all environments — a dev mistake becomes a prod outage.", "Long-lived credentials per environment in CI — assume short-lived roles instead."},
			Quiz:           Quiz{Question: "What stops a dev plan from touching prod state in a multi-account setup?", Options: []string{"Nothing — be careful", "Separate backends and assume-role: the dev run cannot assume prod's role or reach prod's state", "Naming conventions", "A bigger instance"}, Correct: 1, Explanation: "Account boundaries plus separate backends make cross-environment applies impossible, not just discouraged."},
			XP:             15,
		},
		{
			ID: "l3-11-aliases-multiregion", Level: 3, Title: "Provider Aliases and Multi-Region",
			Summary:        "One configuration, several regions: aliases route each resource to its provider configuration.",
			Explanation:    "A provider block with an alias creates a second configuration of the same provider (another region, another account). Resources select it with provider = aws.eu. The default (unaliased) provider handles the rest. Multi-region patterns: a primary region for compute and a secondary for backups or failover, with data replicated between them. The cost of aliases is complexity — every resource must name its provider explicitly, and the graph spans regions.",
			UseCase:        "A bucket is replicated to a second region for disaster recovery: the primary bucket uses the default provider, the replica uses provider = aws.eu.",
			CodeExample:    "provider \"aws\" {\n  region = \"us-east-1\"\n}\n\nprovider \"aws\" {\n  alias  = \"eu\"\n  region = \"eu-west-1\"\n}\n\nresource \"aws_s3_bucket\" \"replica\" {\n  provider = aws.eu\n  bucket   = \"my-assets-replica\"\n}",
			Exercise:       "Add a second provider alias to your project (region cf-local-2) and a resource that uses it. Validate locally — what does the validator check about the alias?",
			ExpectedOutput: "Validation passes; the aliased provider block and the provider = aws.<alias> reference are both present and consistent.",
			Mistakes:       []string{"Forgetting provider = on a resource — it silently lands in the default region.", "Aliasing for one resource and duplicating the whole configuration for it — one alias, many resources."},
			Quiz:           Quiz{Question: "How does a resource choose a non-default provider configuration?", Options: []string{"By its name", "With provider = <alias> in the resource block", "By file location", "It cannot"}, Correct: 1, Explanation: "provider = aws.<alias> routes a resource to the aliased provider configuration."},
			XP:             15,
		},
		{
			ID: "l3-12-zero-downtime", Level: 3, Title: "Zero-Downtime Replacements",
			Summary:        "Replace infrastructure without dropping traffic: create-before-destroy and rolling patterns.",
			Explanation:    "Some replacements are inherently destructive (an instance, a database). Zero downtime comes from ordering: create_before_destroy creates the replacement first, shifts traffic (load balancer, DNS), then destroys the old one. For fleets, rolling replacements (one instance at a time, health-checked between) keep capacity up. The plan shows the replacement as -/+; the lifecycle block and the deployment pattern decide whether traffic notices. Databases are the exception: replacement means data migration, not just ordering — never treat a database replacement as routine.",
			UseCase:        "An AMI update replaces every instance in an autoscaling group one at a time, health-checked, with the load balancer draining connections first.",
			CodeExample:    "resource \"aws_instance\" \"web\" {\n  ami           = data.aws_ami.latest.id\n  instance_type = \"t3.micro\"\n\n  lifecycle {\n    create_before_destroy = true\n  }\n}",
			Exercise:       "Your instance's AMI changes. Describe the replacement sequence with create_before_destroy: what exists at each step, and when is the old instance destroyed?",
			ExpectedOutput: "New instance created first, traffic shifted, old instance destroyed last; at no point is capacity zero.",
			Mistakes:       []string{"create_before_destroy on a uniquely-named resource — the replacement cannot be created while the name is taken.", "Replacing a database with create_before_destroy and no data migration plan — the new database is empty."},
			Quiz:           Quiz{Question: "What does create_before_destroy guarantee?", Options: []string{"No replacement ever happens", "The replacement is created before the old resource is destroyed", "The resource is never destroyed", "Faster applies"}, Correct: 1, Explanation: "Ordering: create first, destroy after — the basis of zero-downtime replacement."},
			XP:             15,
		},
		{
			ID: "l3-13-secrets", Level: 3, Title: "Secrets Management",
			Summary:        "Secrets never live in configuration or state if you can help it — and what to do when they must.",
			Explanation:    "The rule: secrets are referenced, never stored. Configuration references a secrets manager (AWS Secrets Manager, Vault, or the platform's store); the provider reads the secret at apply time and Terraform stores the result in state — still plain text, which is why state backends must be encrypted and access-controlled. Never commit secrets, never put them in tfvars, never log them (sensitive = true hides them from output). Rotation means updating the secret in the manager and letting the next apply pick it up; leaked state files must be treated as leaked secrets.",
			UseCase:        "A database password lives in Secrets Manager. Terraform reads it at apply; the password is never in git, never in a tfvars file, and rotation is a manager operation, not a code change.",
			CodeExample:    "data \"aws_secretsmanager_secret_version\" \"db\" {\n  secret_id = \"prod/db/password\"\n}\n\n# The value is read at apply time and stored in state —\n# protect the state backend as if it held the secret,\n# because it does.",
			Exercise:       "Audit your project files for anything secret-like (passwords, keys, tokens). List where each would be visible (git, plan output, state) and the right home for it.",
			ExpectedOutput: "An audit list with a visibility column and a secrets-manager home for each item; ideally, no secrets in the project at all.",
			Mistakes:       []string{"A 'temporary' secret committed to git — git never forgets; rotate it and purge history.", "Storing the secret in state and sharing state access widely — state access IS secret access."},
			Quiz:           Quiz{Question: "Where should a database password live?", Options: []string{"In terraform.tfvars", "In a secrets manager, referenced by configuration — never in git or tfvars", "In the resource block", "In the state file only"}, Correct: 1, Explanation: "Secrets are referenced from a manager, never stored in configuration or version control."},
			XP:             15,
		},
		{
			ID: "l3-14-cost", Level: 3, Title: "Cost Estimation",
			Summary:        "Plans have prices: estimate before apply, and make cost visible in review.",
			Explanation:    "Every resource in a plan has a price: instance hours, NAT gateway hours plus data processed, storage GB-months. Cost estimation tools (Infracost and friends) annotate the plan diff with monthly costs so reviewers see the bill before approving. The discipline: estimate on every PR, alert on increases above a threshold, and prefer the cheap shape (a NAT instance over a NAT gateway in dev, S3 Intelligent-Tiering, right-sized instances). In CloudForge there is no billing — but the habit transfers: the plan review should always ask 'what does this cost to run?'",
			UseCase:        "A PR adds a NAT gateway per AZ. The cost estimate shows +$100/month; the team keeps one NAT gateway in dev and three in prod.",
			CodeExample:    "# Cost review checklist for a plan:\n# - new always-on resources (NAT GW, instances, load balancers)\n# - data transfer (cross-AZ, cross-region)\n# - storage growth (logs, snapshots, versions)\n# - per-environment: dev should be cheap, prod is the spend",
			Exercise:       "List every resource in your project's plan preview and mark each as always-on, usage-based, or free. Which one would dominate a real monthly bill?",
			ExpectedOutput: "A classified list with the dominant cost named (usually the NAT gateway or always-on instances).",
			Mistakes:       []string{"Reviewing a plan without its cost — the cheapest correct shape and the most expensive one look identical in a diff.", "Forgetting data transfer — it is the line item that surprises teams."},
			Quiz:           Quiz{Question: "When should cost be reviewed?", Options: []string{"After the bill arrives", "On every plan, before apply — the plan is the bill preview", "Only at year end", "Never for dev"}, Correct: 1, Explanation: "The plan diff is the bill preview; cost belongs in plan review, not in the invoice."},
			XP:             15,
		},
		{
			ID: "l3-15-graph-debug", Level: 3, Title: "Graph and Dependency Debugging",
			Summary:        "When Terraform plans something unexpected, read the graph: terraform graph and targeted plans.",
			Explanation:    "Unexpected plans are usually graph surprises: a hidden dependency forcing a replacement, or a missing dependency creating in the wrong order. Debugging tools: terraform graph (the dependency graph as DOT, renderable), terraform plan -target=address (plan only part of the graph — for diagnosis, never as a routine apply), and terraform console (evaluate expressions interactively). The most common surprise: changing an argument that forces replacement (the plan shows -/+) because the provider cannot update it in place — the fix is often a different argument, a lifecycle rule, or accepting the replacement deliberately.",
			UseCase:        "A plan wants to replace a database because a tag changed. terraform console shows the tag is computed from a value that changes every run — the fix is to stop computing the tag.",
			CodeExample:    "$ terraform graph | dot -Tpng > graph.png\n$ terraform console\n> var.cidr_block\n\"10.0.0.0/16\"\n> cidrsubnet(var.cidr_block, 8, 1)\n\"10.0.1.0/24\"",
			Exercise:       "Your plan preview shows an unexpected update. List three possible causes (changed argument, changed dependency, changed data source) and how you would tell them apart from the diff alone.",
			ExpectedOutput: "Three causes with a distinguishing signal for each (which attribute changed, which resource it references, whether a data source refreshed).",
			Mistakes:       []string{"Applying a surprising plan to 'see what happens' — the plan is the diagnosis; apply is the treatment.", "Using -target as a routine workflow — targeted applies leave the rest of the graph unapplied and state inconsistent with the configuration."},
			Quiz:           Quiz{Question: "A plan shows an unexpected -/+ replacement. What do you do first?", Options: []string{"Apply it and see", "Read the diff to find which argument forces replacement, and check the graph", "Delete state", "Re-run init"}, Correct: 1, Explanation: "The diff names the forcing argument; the graph shows why. Diagnose before applying."},
			XP:             15,
		},
		{
			ID: "l3-16-custom-providers", Level: 3, Title: "Custom Providers — an Overview",
			Summary:        "When no provider exists, you can write one — and why you usually should not.",
			Explanation:    "Providers are plugins written against Terraform's plugin protocol (usually in Go with the plugin framework): each resource implements Create/Read/Update/Delete against some API. Writing one is a real engineering project — schema design, state handling, acceptance tests, and a release process. The decision rule: write a provider only when an API is stable, the resources are numerous, and the alternative (scripts, provisioners, or the generic HTTP provider) is worse. The CloudForge provider you have been using is the teaching example: a thin mapping from resource types to the platform's own APIs.",
			UseCase:        "An internal platform exposes a REST API for environments. A small provider turns 'environment' into a Terraform resource, so teams manage environments with the same workflow as everything else.",
			CodeExample:    "# A provider resource maps to API calls:\n#   Create -> POST /api/v1/... (returns an ID)\n#   Read   -> GET  /api/v1/.../{id} (refreshes state)\n#   Update -> PATCH /api/v1/.../{id}\n#   Delete -> DELETE /api/v1/.../{id}\n# The CloudForge provider does exactly this for each type.",
			Exercise:       "Pick one CloudForge API you have used (e.g. SQS queues) and sketch its provider mapping: which HTTP call is Create, Read, Update, Delete, and what is the resource ID?",
			ExpectedOutput: "A four-row CRUD mapping with the real endpoints and the ID each returns.",
			Mistakes:       []string{"Writing a provider for an unstable API — every API change becomes a provider release.", "A provider that shells out to a CLI — the state handling becomes guesswork."},
			Quiz:           Quiz{Question: "What four operations must a provider resource implement?", Options: []string{"Init, plan, apply, destroy", "Create, Read, Update, Delete", "Get, Post, Put, Delete", "Start, stop, restart, status"}, Correct: 1, Explanation: "CRUD: Create, Read (refresh), Update, Delete — the operations Terraform core drives."},
			XP:             15,
		},
		{
			ID: "l3-17-performance", Level: 3, Title: "Performance: Parallelism and Refresh",
			Summary:        "Large configurations plan slowly for specific reasons — and each has a fix.",
			Explanation:    "Plan time grows with resources, provider calls, and refreshes. The levers: parallelism (-parallelism=N, default 10 — raise it when the provider allows), refresh (-refresh=false skips the provider read when you know reality is unchanged — dangerous as a habit, useful in CI for known-clean state), targeted plans for diagnosis, and splitting stacks (fewer resources per plan). The biggest lever is usually fewer resources per state file: a state file with 5,000 resources plans slowly no matter what you tune.",
			UseCase:        "A monolith state with 3,000 resources takes 20 minutes to plan. Splitting it into network/app/data stacks brings each plan under two minutes.",
			CodeExample:    "$ terraform plan -parallelism=20\n$ terraform plan -refresh-only   # refresh state, no diff\n$ terraform apply -refresh=false # skip refresh (CI, known-clean)",
			Exercise:       "Your project is small, so plan time is trivial. Estimate: if each resource took 2 seconds of provider calls and plans ran 10 in parallel, how long would a 200-resource plan take? What would you change first?",
			ExpectedOutput: "About 40 seconds of provider time (200 × 2s / 10 parallel) plus overhead; the first change is splitting the state, not raising parallelism.",
			Mistakes:       []string{"-refresh=false as a daily habit — stale state hides drift until apply time.", "Raising parallelism against a rate-limited provider — the plan gets slower, with throttling errors."},
			Quiz:           Quiz{Question: "What is the biggest lever for slow plans?", Options: []string{"A faster laptop", "Fewer resources per state file (split stacks)", "More parallelism always", "Disabling validation"}, Correct: 1, Explanation: "Plan time scales with resources in state; splitting stacks beats tuning parallelism."},
			XP:             15,
		},
		{
			ID: "l3-18-state-recovery", Level: 3, Title: "Disaster Recovery of State",
			Summary:        "State is the crown jewel: back it up, version it, and know how to rebuild it.",
			Explanation:    "Losing state means losing the record of what Terraform manages: the resources still exist, but Terraform cannot see them. Recovery: restore from the backend's versioning (S3 versioning keeps every state version — enable it), or rebuild by importing each resource (slow, error-prone, and the reason imports are a Level 2 skill). Prevention: versioned backends, backups before surgery, restricted access, and never storing the only copy locally. CloudForge's state store is versioned by design: every backup and restore creates a new version, and history is never rewritten.",
			UseCase:        "A state file is corrupted by a crashed apply. The team restores the previous versioned state, re-plans, and the plan shows the partial apply's gap — which the next apply closes.",
			CodeExample:    "# Recovery order:\n# 1. stop all applies (lock the backend)\n# 2. restore the last known-good state version\n# 3. terraform plan -refresh-only (what does reality say?)\n# 4. reconcile: apply the gap, or import what is missing\n# 5. re-enable applies",
			Exercise:       "Back up your project's state, then simulate a loss: restore the previous version and run a plan preview. What does the preview show, and what would you do next in a real recovery?",
			ExpectedOutput: "A restore to a versioned snapshot and a preview showing the gap between the restored state and the current configuration — the reconcile step.",
			Mistakes:       []string{"No versioning on the state bucket — a corrupt overwrite is unrecoverable.", "Rebuilding state by hand-editing JSON — import each resource instead; the file format is Terraform's, not yours."},
			Quiz:           Quiz{Question: "What is the first step in state recovery?", Options: []string{"Delete the state file", "Stop all applies (lock the backend) and restore the last known-good version", "Apply immediately", "Re-run init"}, Correct: 1, Explanation: "Freeze writes first, then restore a versioned copy, then reconcile with a refresh-only plan."},
			XP:             15,
		},
		{
			ID: "l3-19-team-workflows", Level: 3, Title: "Team Workflows: PR Plans and Approvals",
			Summary:        "The full team loop: branch, plan, review, approve, apply, verify — with separation of duties.",
			Explanation:    "The team workflow is a loop with separation of duties: the author opens a PR with a plan; a reviewer reads the plan (not just the code); an approver (often a different person for prod) approves; CI applies the reviewed plan; the author verifies outputs and closes the loop. The rules that make it work: plans are posted automatically, applies never happen from laptops, prod approvals require a second person, and every apply is logged. The CloudForge flow is the same loop at teaching scale: preview, approve (1-hour TTL), confirm, apply, execution history.",
			UseCase:        "A junior engineer proposes a database change. The plan shows a destroy+create; the reviewer catches it, the change is reworked to an in-place update, and the database survives.",
			CodeExample:    "# Team workflow:\n# author: branch -> fmt/validate/plan -> PR (plan posted)\n# reviewer: reads the PLAN, not just the diff\n# approver: approves (prod: a second person)\n# CI: applies the reviewed plan file, logs everything\n# author: verifies outputs, closes the PR",
			Exercise:       "Walk your last project change through this loop as if a teammate proposed it: what would you check in the plan before approving, and what would make you refuse?",
			ExpectedOutput: "A review checklist (destroys, replacements, cost, drift) and at least one concrete refusal condition (e.g. an unreviewed destroy of a stateful resource).",
			Mistakes:       []string{"Reviewing code but not the plan — the plan is what actually happens.", "The author approving their own prod apply — separation of duties exists for the change you are sure about."},
			Quiz:           Quiz{Question: "What should a reviewer read first?", Options: []string{"The code diff only", "The plan — it is what will actually happen", "The state file", "The provider changelog"}, Correct: 1, Explanation: "The plan is the change; the code diff is the intent. Review the plan."},
			XP:             15,
		},
		{
			ID: "l3-20-migration", Level: 3, Title: "Migrating to Terraform",
			Summary:        "Adopting Terraform on existing infrastructure: import, adopt, and cut over without downtime.",
			Explanation:    "Migration is a project, not a command: inventory what exists (and what is actually used — dead resources are decommissioned, not imported), write configuration to match reality, import in dependency order (network first), and iterate plan-until-clean on a copy of state before touching the real one. Cut over gradually: one stack at a time, with the old process (console/runbook) still available until the new one proves itself. The CloudFormation comparison: CloudFormation is AWS-only and template-driven; Terraform is multi-provider and state-driven — migration between them is a rewrite plus import, and the same plan-until-clean discipline applies.",
			UseCase:        "A team migrates a hand-built VPC: inventory, configuration, import, plan-until-clean, then the console workflow is retired for that stack.",
			CodeExample:    "# Migration order:\n# 1. inventory (what exists? what is used?)\n# 2. decommission the dead\n# 3. write configuration to match reality\n# 4. import in dependency order, plan until clean\n# 5. cut over one stack at a time\n# 6. retire the old workflow",
			Exercise:       "Inventory your lab: list the resources that exist in your lab (from the explorer) and mark each as 'would import', 'would recreate', or 'would decommission'. Why did you choose each?",
			ExpectedOutput: "An inventory with a decision per resource and a reason — the migration plan in miniature.",
			Mistakes:       []string{"Importing everything including dead resources — migration is the chance to decommission.", "Big-bang cutover — migrate one stack, prove it, then the next."},
			Quiz:           Quiz{Question: "What is the first step in migrating existing infrastructure to Terraform?", Options: []string{"Import everything", "Inventory what exists and decommission what is dead", "Delete the old infrastructure", "Run apply"}, Correct: 1, Explanation: "Inventory first: you cannot manage what you have not listed, and migration is the time to remove dead resources."},
			XP:             15,
		},
	},
}

var level4Projects = []Project{
	{
		ID: "p4-01-vpc", Level: 4, Title: "VPC Network",
		Description: "Build a complete VPC with public and private subnets, an internet gateway, and route tables — the foundation every later project stands on.",
		Objectives:  []string{"Design a /16 VPC with public and private /24 subnets", "Wire an internet gateway and route tables", "Prove the topology with validation and a plan preview"},
		Tasks:       []string{"Write the VPC, two public and two private subnets, IGW, and route tables", "Validate and format the configuration", "Run a plan preview and review every change", "Link a lab and run init/plan through the container runner"},
		Criteria:    []string{"Plan preview shows the VPC, 4 subnets, IGW, and route tables as creates", "Local validation reports no errors", "No resource is created before its dependencies"},
		StarterFiles: map[string]string{
			"main.tf":      "resource \"cloudforge_vpc\" \"main\" {\n  cidr_block = \"10.0.0.0/16\"\n  name       = \"main\"\n}\n\nresource \"cloudforge_subnet\" \"public_a\" {\n  vpc_id     = cloudforge_vpc.main.id\n  cidr_block = \"10.0.1.0/24\"\n  public     = true\n}\n",
			"variables.tf": "variable \"cidr_block\" {\n  description = \"CIDR for the VPC\"\n  type        = string\n  default     = \"10.0.0.0/16\"\n}\n",
			"outputs.tf":   "output \"vpc_id\" {\n  description = \"ID of the main VPC\"\n  value       = cloudforge_vpc.main.id\n}\n",
			"providers.tf": "terraform {\n  required_providers {\n    aws = {\n      source  = \"hashicorp/aws\"\n      version = \"~> 5.0\"\n    }\n  }\n}\n",
		},
		XP: 50,
	},
	{
		ID: "p4-02-compute", Level: 4, Title: "Compute Instance",
		Description: "Launch a compute instance into your VPC with a security group, outputs for its address, and a lifecycle that protects it from accidental replacement.",
		Objectives:  []string{"Launch an instance into a subnet with the right security group", "Expose its private IP as an output", "Guard the instance with a lifecycle rule"},
		Tasks:       []string{"Add a security group (SSH + HTTP) and an instance to the VPC project", "Add outputs for the instance id and private IP", "Run a plan preview and check the dependency order"},
		Criteria:    []string{"Instance references the subnet and security group (implicit dependencies)", "Plan preview shows instance + security group as creates", "Outputs expose id and private IP"},
		StarterFiles: map[string]string{
			"main.tf":    "resource \"cloudforge_security_group\" \"web\" {\n  vpc_id = cloudforge_vpc.main.id\n  name   = \"web\"\n}\n\nresource \"cloudforge_instance\" \"web\" {\n  subnet_id     = cloudforge_subnet.public_a.id\n  instance_type = \"micro\"\n  name          = \"web\"\n}\n",
			"outputs.tf": "output \"instance_id\" {\n  value = cloudforge_instance.web.id\n}\n\noutput \"instance_private_ip\" {\n  value = cloudforge_instance.web.private_ip\n}\n",
		},
		XP: 50,
	},
	{
		ID: "p4-03-s3", Level: 4, Title: "S3 Bucket",
		Description: "Create a versioned, sensibly-named object storage bucket with outputs — and learn what the bucket does not do (no public access by default).",
		Objectives:  []string{"Create a bucket with versioning and a unique name", "Understand the lite-mode limit (MinIO is full-mode only)", "Wire the bucket name into an output"},
		Tasks:       []string{"Write the bucket resource with versioning enabled", "Validate locally and run a plan preview", "In full mode, link a lab and apply; in lite mode, record the honest 503 for the MinIO-backed step"},
		Criteria:    []string{"Bucket name is globally sensible and parameterised by a variable", "Validation passes; plan preview shows the bucket as a create", "The lite-mode MinIO limit is stated in the project notes, not hidden"},
		StarterFiles: map[string]string{
			"main.tf":      "resource \"cloudforge_s3_bucket\" \"assets\" {\n  bucket     = var.bucket_name\n  versioning = true\n}\n",
			"variables.tf": "variable \"bucket_name\" {\n  description = \"Name of the assets bucket\"\n  type        = string\n  default     = \"my-assets\"\n}\n",
			"outputs.tf":   "output \"bucket_name\" {\n  value = cloudforge_s3_bucket.assets.bucket\n}\n",
		},
		XP: 50,
	},
	{
		ID: "p4-04-three-tier", Level: 4, Title: "Three-Tier Application",
		Description: "Compose the VPC, compute, and storage projects into a three-tier app: public load-balancer tier, private app tier, and a data tier — as modules.",
		Objectives:  []string{"Compose network, compute, and data modules into one system", "Keep tiers separated (public vs private subnets)", "Prove the composition with a single plan preview"},
		Tasks:       []string{"Extract network and compute into modules/", "Wire module outputs to inputs (vpc_id, subnet IDs)", "Run a plan preview of the whole system and review the tier separation"},
		Criteria:    []string{"Modules compose through outputs only — no cross-module internals", "App-tier resources sit in private subnets", "One plan preview covers the whole system"},
		StarterFiles: map[string]string{
			"main.tf":                 "module \"network\" {\n  source     = \"./modules/network\"\n  cidr_block = \"10.0.0.0/16\"\n}\n\nmodule \"compute\" {\n  source    = \"./modules/compute\"\n  vpc_id    = module.network.vpc_id\n  subnet_id = module.network.public_subnet_id\n}\n",
			"modules/network/main.tf": "resource \"cloudforge_vpc\" \"main\" {\n  cidr_block = var.cidr_block\n}\n\noutput \"vpc_id\" {\n  value = cloudforge_vpc.main.id\n}\n\noutput \"public_subnet_id\" {\n  value = cloudforge_subnet.public.id\n}\n\nresource \"cloudforge_subnet\" \"public\" {\n  vpc_id     = cloudforge_vpc.main.id\n  cidr_block = \"10.0.1.0/24\"\n  public     = true\n}\n",
		},
		XP: 100,
	},
	{
		ID: "p4-05-serverless", Level: 4, Title: "Serverless Application",
		Description: "Build a serverless app: a queue feeding a function, with an IAM role that grants exactly the permissions the function needs.",
		Objectives:  []string{"Create a queue, a function, and the IAM role between them", "Apply least privilege to the role", "Trigger the function and read its execution log"},
		Tasks:       []string{"Write the queue, function, and IAM role resources", "Keep the role's permissions minimal and document why", "Validate, preview, and (full mode) apply; check the function runs"},
		Criteria:    []string{"Function references the queue and the role (dependency graph is correct)", "Role policy grants only what the function uses", "Plan preview shows all three resources as creates"},
		StarterFiles: map[string]string{
			"main.tf": "resource \"cloudforge_sqs_queue\" \"jobs\" {\n  name = \"jobs\"\n}\n\nresource \"cloudforge_iam_role\" \"worker\" {\n  name = \"worker\"\n}\n\nresource \"cloudforge_lambda_function\" \"worker\" {\n  function_name = \"worker\"\n  runtime       = \"python3.9\"\n  handler       = \"index.handler\"\n}\n",
		},
		XP: 100,
	},
	{
		ID: "p4-06-complete-env", Level: 4, Title: "Complete Environment",
		Description: "Assemble everything into a complete environment with dev/prod separation: networking, compute, storage, messaging, and serverless — one reviewed plan.",
		Objectives:  []string{"Assemble all prior projects into one environment", "Separate dev and prod with workspaces or tfvars", "Run the full team workflow: preview, approve, confirm, apply"},
		Tasks:       []string{"Compose the VPC, compute, S3, and serverless modules", "Parameterise by environment (variables + tfvars per env)", "Walk the full workflow: plan preview -> approve -> confirm -> apply (or record the honest 503 without Docker)"},
		Criteria:    []string{"One plan preview covers the whole environment", "Dev and prod differ only by inputs, not by code", "The apply was approval- and confirmation-gated (or honestly unavailable)"},
		StarterFiles: map[string]string{
			"main.tf":          "module \"network\" {\n  source = \"./modules/network\"\n}\n\nmodule \"compute\" {\n  source    = \"./modules/compute\"\n  vpc_id    = module.network.vpc_id\n  subnet_id = module.network.public_subnet_id\n}\n\nmodule \"storage\" {\n  source = \"./modules/storage\"\n}\n",
			"terraform.tfvars": "environment = \"dev\"\n",
		},
		XP: 150,
	},
	{
		ID: "p4-07-reusable-modules", Level: 4, Title: "Reusable Modules",
		Description: "Turn the complete environment into a reusable module with a clean contract: documented variables, validated inputs, minimal outputs — ready for another team to use.",
		Objectives:  []string{"Package a working system as a module with a documented contract", "Validate every input and expose only the outputs callers need", "Version it: the module's consumers pin a version"},
		Tasks:       []string{"Extract the environment into modules/<name>/ with variables.tf, outputs.tf, and a README", "Add validation blocks to every variable", "Write the consumer's module block with a pinned source version"},
		Criteria:    []string{"Every variable has a type, description, and (where it matters) validation", "Outputs are minimal — only what a caller needs", "The module is usable from a fresh root configuration with three inputs or fewer"},
		StarterFiles: map[string]string{
			"modules/environment/variables.tf": "variable \"environment\" {\n  description = \"Deployment environment\"\n  type        = string\n\n  validation {\n    condition     = contains([\"dev\", \"staging\", \"prod\"], var.environment)\n    error_message = \"environment must be dev, staging or prod.\"\n  }\n}\n\nvariable \"cidr_block\" {\n  description = \"CIDR for the VPC\"\n  type        = string\n  default     = \"10.0.0.0/16\"\n}\n",
			"modules/environment/README.md":    "# environment module\n\nCreates a complete environment: VPC, subnets, compute, storage.\n\n## Inputs\n\n- `environment` (required): dev, staging, or prod.\n- `cidr_block` (optional, default 10.0.0.0/16).\n\n## Outputs\n\n- `vpc_id`, `public_subnet_id`.\n",
		},
		XP: 150,
	},
}
