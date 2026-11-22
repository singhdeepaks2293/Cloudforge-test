#!/usr/bin/env bash
# CloudForge Phase 4 live E2E walk (sandbox, lite mode).
# API :8081 + worker :8083 + engine :50051 (SIMULATED) + real Postgres
# :5432 + real Redis 7.0.15 :6379 + frontend preview :8080 / dev :5173.
# (No DynamoDB Local here — no Java in this sandbox VM; no Phase 4
# flow needs it. No Docker/KVM.)
# Every step prints its real HTTP status + body. Failures are recorded,
# not hidden: the script continues and exits non-zero at the end if any
# EXPECTED step failed.
API=http://localhost:8081
LOG=/tmp/cf-live/transcript-phase4.txt
FAIL=0
: > "$LOG"

step() { echo "=== $1" | tee -a "$LOG"; }
expect() { # expect <want-status> <label> <actual-status>
  if [ "$3" = "$1" ]; then echo "  PASS $2: HTTP $3" | tee -a "$LOG";
  else echo "  FAIL $2: got HTTP $3, want $1" | tee -a "$LOG"; FAIL=1; fi
}
jget() { python3 -c "import json,sys; d=json.load(sys.stdin); print(d$1)" 2>/dev/null; }

call() { # call <method> <path> <token|-> <body|-> ; prints body, sets CODE
  local method=$1 path=$2 token=$3 body=$4
  local args=(-s -w "\n%{http_code}" "$API$path" -X "$method")
  [ "$token" != "-" ] && args+=(-H "Authorization: Bearer $token")
  [ "$body" != "-" ] && args+=(-H "Content-Type: application/json" -d "$body")
  local out; out=$(curl "${args[@]}")
  CODE=$(echo "$out" | tail -n 1)
  BODY=$(echo "$out" | sed '$d')
  echo "$BODY" | tee -a "$LOG" | head -c 1500; echo | tee -a "$LOG"
}

# waitfor <url> <jq-field> <want> <label> — polls up to 60s.
waitfor() {
  local url=$1 field=$2 want=$3 label=$4 i=0 last=""
  for i in $(seq 1 60); do
    last=$(curl -s -H "Authorization: Bearer $TOKEN" "$API$url")
    local v; v=$(echo "$last" | python3 -c "import json,sys; print(json.load(sys.stdin)$field)" 2>/dev/null)
    if [ "$v" = "$want" ]; then echo "  PASS $label: $field == $want after ${i}s" | tee -a "$LOG"; return 0; fi
    sleep 1
  done
  echo "  FAIL $label: $field never became $want; last=$last" | tee -a "$LOG"; FAIL=1; return 1
}

step "0. health (lite mode, real Postgres + real Redis + engine SIMULATED)"
call GET /api/v1/health - -
expect 200 "health" "$CODE"

step "1. register admin; unauthenticated Phase 4 call is 401"
RUN=$(date +%s)
call POST /api/v1/auth/register - '{"email":"p4admin-'"$RUN"'@cloudforge.local","name":"P4 Admin","password":"correct-horse"}'
expect 201 "register" "$CODE"
TOKEN=$(echo "$BODY" | jget "['token']")
call GET /api/v1/aws/ecr/repositories - -
expect 401 "unauthenticated ECR list" "$CODE"

step "2. service registry lists the Phase 4 services with fidelity"
call GET /api/v1/services - -
echo "$BODY" | python3 -c "
import json,sys
d=json.load(sys.stdin)
svcs={s['id']:s for s in d.get('services',[])}
for k in ['ecr','ecs','fargate','eks','codebuild','codepipeline','codedeploy','cloudformation']:
    s=svcs.get(k,{})
    print(' ',k,'->',s.get('status'),'/',s.get('fidelity'),'|',s.get('route',''))
" | tee -a "$LOG"

step "3. ECR — failure: bad repository name is 400, nothing created"
call POST /api/v1/aws/ecr/repositories "$TOKEN" '{"name":"Bad_Name"}'
expect 400 "bad repo name" "$CODE"
call GET /api/v1/aws/ecr/repositories "$TOKEN" -
echo "$BODY" | python3 -c "import json,sys; print('  repos after bad create:', len(json.load(sys.stdin).get('repositories',[])))" | tee -a "$LOG"

step "4. ECR — create repo, push image, list, scan, registry API"
REPONAME="demo/app-$RUN"
call POST /api/v1/aws/ecr/repositories "$TOKEN" '{"name":"'$REPONAME'","lifecycle_rules":[{"keep_last_n_tagged":1}]}'
expect 201 "create repo" "$CODE"
REPO=$(echo "$BODY" | jget "['id']")
LAYER=$(printf 'hello from CloudForge ECR' | base64 -w0)
call POST /api/v1/aws/ecr/repositories/$REPO/images "$TOKEN" '{"tag":"v1","layers":["'"$LAYER"'"]}'
expect 201 "push image v1 (layers shape)" "$CODE"
IMG=$(echo "$BODY" | jget "['id']")
DIGEST=$(echo "$BODY" | jget "['digest']")
call GET /api/v1/aws/ecr/repositories/$REPO/images "$TOKEN" -
expect 200 "list images" "$CODE"
call POST /api/v1/aws/ecr/images/$IMG/scan "$TOKEN" -
expect 200 "scan image" "$CODE"
echo "$BODY" | python3 -c "import json,sys; d=json.load(sys.stdin); print('  scan_status:', d.get('scan_status'), '|', d.get('scan_note'))" | tee -a "$LOG"
# Docker Registry HTTP API subset, unauthenticated (local-only).
curl -s http://localhost:8081/v2/ -w "\nregistry /v2/: %{http_code}\n" | tee -a "$LOG"
curl -s "http://localhost:8081/v2/$REPONAME/tags/list" | tee -a "$LOG"; echo
curl -s "http://localhost:8081/v2/$REPONAME/manifests/v1" | tee -a "$LOG" | head -c 600; echo
curl -s "http://localhost:8081/v2/$REPONAME/blobs/$DIGEST" -o /tmp/cf-live/blob.bin -w "blob download: %{http_code} %{size_download} bytes\n" | tee -a "$LOG"
echo "blob content: $(cat /tmp/cf-live/blob.bin)" | tee -a "$LOG"
# Failure: a manifest referencing a blob that was never pushed is 400.
NEVER_DIGEST="sha256:0000000000000000000000000000000000000000000000000000000000000000"
NEVER_CFG="sha256:1111111111111111111111111111111111111111111111111111111111111111"
call POST /api/v1/aws/ecr/repositories/$REPO/images "$TOKEN" '{"tag":"bad","manifest":{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json","config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"'$NEVER_CFG'","size":5},"layers":[{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","digest":"'$NEVER_DIGEST'","size":5}]},"blobs":[]}'
expect 400 "push with manifest referencing missing blob" "$CODE"

step "5. ECR — lifecycle keeps only the newest tagged image"
call POST /api/v1/aws/ecr/repositories/$REPO/images "$TOKEN" '{"tag":"v2","layers":["'"$LAYER"'"]}'
expect 201 "push image v2" "$CODE"
call GET /api/v1/aws/ecr/repositories/$REPO/images "$TOKEN" -
echo "$BODY" | python3 -c "import json,sys; print('  images after lifecycle (keep 1):', [i['tag'] for i in json.load(sys.stdin).get('images',[])])" | tee -a "$LOG"
TAGS=$(echo "$BODY" | python3 -c "import json,sys; print(','.join(sorted(i['tag'] for i in json.load(sys.stdin).get('images',[]))))")
if [ "$TAGS" = "v2" ]; then echo "  PASS lifecycle kept only v2 (shorthand rule shape)" | tee -a "$LOG"; else echo "  FAIL lifecycle kept only v2: tags=$TAGS" | tee -a "$LOG"; FAIL=1; fi
IMG2=$(echo "$BODY" | jget "['images'][0]['id']")
call POST /api/v1/aws/ecr/repositories/$REPO/lifecycle "$TOKEN" -
expect 200 "lifecycle evaluate" "$CODE"

step "6. ECS — failure: task definition without a command is 400"
call POST /api/v1/aws/ecs/task-definitions "$TOKEN" '{"family":"nocmd","container_definitions":[{"name":"nocmd","image":"demo/app:v1","cpu":256,"memory_mb":128}]}'
expect 400 "task def without command" "$CODE"
call POST /api/v1/aws/ecs/clusters "$TOKEN" '{"name":"web-'$RUN'"}'
expect 201 "create cluster" "$CODE"
CLUSTER=$(echo "$BODY" | jget "['id']")
call POST /api/v1/aws/ecs/task-definitions "$TOKEN" '{"family":"echoer","container_definitions":[{"name":"echoer","image":"demo/app:v1","command":["echo","hello-task"],"cpu":256,"memory_mb":128}]}'
expect 201 "register task def" "$CODE"
TD=$(echo "$BODY" | jget "['id']")
call POST /api/v1/aws/ecs/tasks/run "$TOKEN" '{"task_definition_id":"'$TD'","cluster_id":"'$CLUSTER'","launch_type":"EC2","count":1}'
expect 201 "run task" "$CODE"
TASK=$(echo "$BODY" | jget "['tasks'][0]['id']")
waitfor /api/v1/aws/ecs/tasks/$TASK "['status']" "STOPPED" "echo task stops with real exit code"
call GET /api/v1/aws/ecs/tasks/$TASK "$TOKEN" -
echo "$BODY" | python3 -c "import json,sys; d=json.load(sys.stdin); print('  exit_code:', d.get('exit_code'), '| running:', d.get('running'))" | tee -a "$LOG"
call GET /api/v1/aws/ecs/tasks/$TASK/logs "$TOKEN" -
expect 200 "task logs" "$CODE"
echo "$BODY" | python3 -c "import json,sys; print('  log:', repr(json.load(sys.stdin).get('logs','')))" | tee -a "$LOG"

step "7. ECS — Fargate run (no cluster) + service with desired count"
call POST /api/v1/aws/ecs/tasks/run "$TOKEN" '{"task_definition_id":"'$TD'","launch_type":"FARGATE","count":1}'
expect 201 "fargate run" "$CODE"
FTASK=$(echo "$BODY" | jget "['tasks'][0]['id']")
waitfor /api/v1/aws/ecs/tasks/$FTASK "['status']" "STOPPED" "fargate task stops"
# Services use a long-lived task (sleep 60): an instantly-exiting
# command under a service would trip the reconcile circuit breaker
# (by design — that path is tested separately in step 7b).
call POST /api/v1/aws/ecs/task-definitions "$TOKEN" '{"family":"sleeper","container_definitions":[{"name":"sleeper","image":"demo/app:v1","command":["sleep","60"],"cpu":256,"memory_mb":128}]}'
expect 201 "register sleeper task def" "$CODE"
TDSLEEP=$(echo "$BODY" | jget "['id']")
call POST /api/v1/aws/ecs/services "$TOKEN" '{"name":"web-svc-'$RUN'","cluster_id":"'$CLUSTER'","task_definition_id":"'$TDSLEEP'","desired_count":1,"launch_type":"EC2"}'
expect 201 "create service" "$CODE"
SVC=$(echo "$BODY" | jget "['id']")
sleep 2
call GET /api/v1/aws/ecs/services/$SVC "$TOKEN" -
expect 200 "get service" "$CODE"
echo "$BODY" | python3 -c "import json,sys; d=json.load(sys.stdin); print('  running_count:', d.get('running_count'), 'desired:', d.get('desired_count'))" | tee -a "$LOG"
# Failure: stopping a service-managed task is 409.
SMTASK=$(curl -s -H "Authorization: Bearer $TOKEN" "$API/api/v1/aws/ecs/tasks?service_id=$SVC" | python3 -c "import json,sys; t=[x for x in json.load(sys.stdin).get('tasks',[]) if x.get('status')=='RUNNING']; print(t[0]['id'] if t else '')" 2>/dev/null)
[ -n "$SMTASK" ] && call POST /api/v1/aws/ecs/tasks/$SMTASK/stop "$TOKEN" - && expect 409 "stop service-managed task" "$CODE"
# Scale to 0, then delete the service.
call PUT /api/v1/aws/ecs/services/$SVC "$TOKEN" '{"desired_count":0}'
expect 200 "scale service to 0" "$CODE"
sleep 2
call DELETE /api/v1/aws/ecs/services/$SVC "$TOKEN" -
expect 200 "delete service" "$CODE"

step "7b. ECS circuit breaker — an instantly-exiting service throttles instead of looping"
call POST /api/v1/aws/ecs/services "$TOKEN" '{"name":"echo-loop-'$RUN'","cluster_id":"'$CLUSTER'","task_definition_id":"'$TD'","desired_count":1,"launch_type":"EC2"}'
expect 201 "create echo service" "$CODE"
LOOPSVC=$(echo "$BODY" | jget "['id']")
sleep 4
call GET /api/v1/aws/ecs/tasks?service_id=$LOOPSVC "$TOKEN" -
echo "$BODY" | python3 -c "import json,sys; print('  tasks created for echo service in ~4s:', len(json.load(sys.stdin).get('tasks',[])))" | tee -a "$LOG"
call GET /api/v1/aws/ecs/services/$LOOPSVC "$TOKEN" -
echo "$BODY" | python3 -c "import json,sys; d=json.load(sys.stdin); print('  reconcile_throttled:', d.get('reconcile_throttled'), '|', d.get('reconcile_throttle_reason',''))" | tee -a "$LOG"
call PUT /api/v1/aws/ecs/services/$LOOPSVC "$TOKEN" '{"desired_count":0}'
expect 200 "scale echo service to 0 (clears throttle)" "$CODE"
call DELETE /api/v1/aws/ecs/services/$LOOPSVC "$TOKEN" -
expect 200 "delete echo service" "$CODE"

step "8. EKS — info endpoint: UNSUPPORTED with the learning path"
call GET /api/v1/aws/eks/info "$TOKEN" -
expect 200 "eks info" "$CODE"
echo "$BODY" | python3 -c "import json,sys; d=json.load(sys.stdin); print('  fidelity:', d.get('fidelity'), '| supported:', d.get('supported'), '| concepts:', [c['concept'] for c in d.get('learning_path',[])])" | tee -a "$LOG"

step "9. CodeBuild — failure: remote source is 400; real build succeeds"
call POST /api/v1/aws/codebuild/projects "$TOKEN" '{"name":"remote-'$RUN'","source_path":"https://github.com/example/repo.git"}'
expect 400 "remote source rejected" "$CODE"
call POST /api/v1/aws/codebuild/projects "$TOKEN" '{"name":"good-'$RUN'","source_path":"/tmp/cf-live/workdir/p4git"}'
expect 201 "create git project" "$CODE"
PROJ=$(echo "$BODY" | jget "['id']")
call POST /api/v1/aws/codebuild/projects/$PROJ/builds "$TOKEN" -
expect 202 "start build" "$CODE"
BUILD=$(echo "$BODY" | jget "['id']")
waitfor /api/v1/aws/codebuild/builds/$BUILD "['status']" "SUCCEEDED" "git build succeeds"
call GET /api/v1/aws/codebuild/builds/$BUILD/logs "$TOKEN" -
expect 200 "build logs" "$CODE"
echo "$BODY" | python3 -c "import json,sys; print('  log contains build-ok:', 'build-ok' in json.load(sys.stdin).get('logs',''))" | tee -a "$LOG"
call GET /api/v1/aws/codebuild/builds/$BUILD "$TOKEN" -
echo "$BODY" | python3 -c "import json,sys; d=json.load(sys.stdin); print('  artifacts:', d.get('artifacts'), '| commit:', str(d.get('commit'))[:7])" | tee -a "$LOG"
# Failure: a build that exits non-zero is FAILED with real logs, not SUCCEEDED.
call POST /api/v1/aws/codebuild/projects "$TOKEN" '{"name":"bad-'$RUN'","source_path":"/tmp/cf-live/workdir/p4fail"}'
expect 201 "create failing project" "$CODE"
BADPROJ=$(echo "$BODY" | jget "['id']")
call POST /api/v1/aws/codebuild/projects/$BADPROJ/builds "$TOKEN" -
BADBUILD=$(echo "$BODY" | jget "['id']")
waitfor /api/v1/aws/codebuild/builds/$BADBUILD "['status']" "FAILED" "failing build is FAILED"
call GET /api/v1/aws/codebuild/builds/$BADBUILD/logs "$TOKEN" -
echo "$BODY" | python3 -c "import json,sys; print('  failing log contains failing-now:', 'failing-now' in json.load(sys.stdin).get('logs',''))" | tee -a "$LOG"

step "10. CodePipeline — source → build → deploy (ECS), and a Terraform deploy that honestly fails"
call POST /api/v1/aws/ecs/task-definitions "$TOKEN" '{"family":"sleeper","container_definitions":[{"name":"sleeper","image":"demo/app:v2","command":["sleep","60"],"cpu":256,"memory_mb":128}]}'
expect 201 "register task def v2" "$CODE"
TD2=$(echo "$BODY" | jget "['id']")
call POST /api/v1/aws/ecs/services "$TOKEN" '{"name":"pipe-svc-'$RUN'","cluster_id":"'$CLUSTER'","task_definition_id":"'$TDSLEEP'","desired_count":1,"launch_type":"EC2"}'
expect 201 "create pipeline target service" "$CODE"
PSVC=$(echo "$BODY" | jget "['id']")
call POST /api/v1/aws/codepipeline/pipelines "$TOKEN" '{"name":"pipe-'$RUN'","stages":[{"name":"Source","type":"source","source_path":"/tmp/cf-live/workdir/p4git"},{"name":"Build","type":"build","build_project_name":"good-'$RUN'"},{"name":"Deploy","type":"deploy","target":"ecs-service","service_id":"'$PSVC'","task_definition_id":"'$TD2'"}]}'
expect 201 "create pipeline" "$CODE"
PIPE=$(echo "$BODY" | jget "['id']")
call POST /api/v1/aws/codepipeline/pipelines/$PIPE/executions "$TOKEN" -
expect 202 "start execution" "$CODE"
EXEC=$(echo "$BODY" | jget "['id']")
waitfor /api/v1/aws/codepipeline/executions/$EXEC "['status']" "SUCCEEDED" "pipeline execution succeeds"
call GET /api/v1/aws/codepipeline/executions/$EXEC "$TOKEN" -
echo "$BODY" | python3 -c "import json,sys; print('  stages:', [(s['name'], s['status']) for s in json.load(sys.stdin).get('stages',[])])" | tee -a "$LOG"
call GET /api/v1/aws/ecs/services/$PSVC "$TOKEN" -
echo "$BODY" | python3 -c "import json,sys; d=json.load(sys.stdin); print('  service task_definition_id now == TD2:', d.get('task_definition_id') == '$TD2')" | tee -a "$LOG"
# Terraform deploy target: no Docker here, so the execution FAILS honestly.
call POST /api/v1/aws/codepipeline/pipelines "$TOKEN" '{"name":"tfpipe-'$RUN'","stages":[{"name":"Source","type":"source","source_path":"/tmp/cf-live/workdir/p4git"},{"name":"Deploy","type":"deploy","target":"terraform"}]}'
expect 201 "create terraform pipeline" "$CODE"
TFPIPE=$(echo "$BODY" | jget "['id']")
call POST /api/v1/aws/codepipeline/pipelines/$TFPIPE/executions "$TOKEN" -
TFEXEC=$(echo "$BODY" | jget "['id']")
waitfor /api/v1/aws/codepipeline/executions/$TFEXEC "['status']" "FAILED" "terraform deploy execution fails honestly"
call GET /api/v1/aws/codepipeline/executions/$TFEXEC "$TOKEN" -
echo "$BODY" | python3 -c "import json,sys; print('  failure_reason:', json.load(sys.stdin).get('failure_reason'))" | tee -a "$LOG"

step "11. CodeDeploy — deploy a new task definition, then roll back"
call POST /api/v1/aws/ecs/task-definitions "$TOKEN" '{"family":"sleeper","container_definitions":[{"name":"sleeper","image":"demo/app:v3","command":["sleep","60"],"cpu":256,"memory_mb":128}]}'
expect 201 "register task def v3" "$CODE"
TD3=$(echo "$BODY" | jget "['id']")
call POST /api/v1/aws/codedeploy/applications "$TOKEN" '{"name":"web-app-'$RUN'","target_type":"ecs-service","target_id":"'$PSVC'"}'
expect 201 "create application" "$CODE"
APP=$(echo "$BODY" | jget "['id']")
call POST /api/v1/aws/codedeploy/applications/$APP/deployments "$TOKEN" '{"task_definition_id":"'$TD3'"}'
expect 202 "create deployment" "$CODE"
DEP=$(echo "$BODY" | jget "['id']")
waitfor /api/v1/aws/codedeploy/deployments/$DEP "['status']" "SUCCEEDED" "deployment succeeds"
call GET /api/v1/aws/ecs/services/$PSVC "$TOKEN" -
echo "$BODY" | python3 -c "import json,sys; d=json.load(sys.stdin); print('  service task_definition_id now == TD3:', d.get('task_definition_id') == '$TD3')" | tee -a "$LOG"
call POST /api/v1/aws/codedeploy/deployments/$DEP/rollback "$TOKEN" -
expect 202 "rollback" "$CODE"
RDEP=$(echo "$BODY" | jget "['id']")
waitfor /api/v1/aws/codedeploy/deployments/$RDEP "['status']" "SUCCEEDED" "rollback succeeds"
call GET /api/v1/aws/ecs/services/$PSVC "$TOKEN" -
echo "$BODY" | python3 -c "import json,sys; d=json.load(sys.stdin); print('  service task_definition_id after rollback == TD2:', d.get('task_definition_id') == '$TD2')" | tee -a "$LOG"

step "12. CloudFormation — failure: unsupported type is 400, nothing stored"
call POST /api/v1/aws/cloudformation/stacks "$TOKEN" '{"name":"bad-'$RUN'","template":"{\"Resources\": {\"Box\": {\"Type\": \"AWS::EC2::Instance\", \"Properties\": {}}}}"}'
expect 400 "unsupported type stack" "$CODE"
call POST /api/v1/aws/cloudformation/validate-template "$TOKEN" '{"template":"{\"Resources\": {\"Box\": {\"Type\": \"AWS::EC2::Instance\", \"Properties\": {}}}}"}'
expect 200 "validate unsupported template" "$CODE"
echo "$BODY" | python3 -c "import json,sys; d=json.load(sys.stdin); print('  valid:', d.get('valid'), '| problems:', d.get('problems'))" | tee -a "$LOG"

step "13. CloudFormation — create stack, change set, execute, delete"
CFN_TPL='{"Resources": {"Queue": {"Type": "AWS::SQS::Queue", "Properties": {"QueueName": "cfn-orders-'$RUN'"}}, "Topic": {"Type": "AWS::SNS::Topic", "Properties": {"TopicName": "cfn-events-'$RUN'"}}, "Fn": {"Type": "AWS::Lambda::Function", "Properties": {"FunctionName": "cfn-fn-'$RUN'", "Runtime": "python3", "Code": {"ZipFile": "import json;print(json.dumps({\"ok\":True}))"}}}}}'
# The API takes the template as a JSON string (same contract as the
# console textarea), so build the body with python, not by splicing
# the template object into the JSON.
CFN_BODY=$(CFN_TPL="$CFN_TPL" python3 -c "import json,os; print(json.dumps({'name':'demo-$RUN','template':os.environ['CFN_TPL']}))")
call POST /api/v1/aws/cloudformation/stacks "$TOKEN" "$CFN_BODY"
expect 201 "create stack" "$CODE"
STACK=$(echo "$BODY" | jget "['id']")
echo "$BODY" | python3 -c "import json,sys; d=json.load(sys.stdin); print('  status:', d.get('status'), '| resources:', list((d.get('resources') or {}).keys()))" | tee -a "$LOG"
call GET /api/v1/aws/sqs/queues "$TOKEN" -
echo "$BODY" | python3 -c "import json,sys; print('  SQS queues now include cfn-orders:', any('cfn-orders' in q.get('name','') for q in json.load(sys.stdin).get('queues',[])))" | tee -a "$LOG"
call GET /api/v1/aws/cloudformation/stacks/$STACK/events "$TOKEN" -
expect 200 "stack events" "$CODE"
# Change set: add a second queue + an unsupported EC2 instance.
CS_TPL='{"Resources": {"Queue": {"Type": "AWS::SQS::Queue", "Properties": {"QueueName": "cfn-orders-'$RUN'"}}, "Topic": {"Type": "AWS::SNS::Topic", "Properties": {"TopicName": "cfn-events-'$RUN'"}}, "Fn": {"Type": "AWS::Lambda::Function", "Properties": {"FunctionName": "cfn-fn-'$RUN'", "Runtime": "python3", "Code": {"ZipFile": "import json;print(json.dumps({\"ok\":True}))"}}}, "Queue2": {"Type": "AWS::SQS::Queue", "Properties": {"QueueName": "cfn-orders2-'$RUN'"}}, "Box": {"Type": "AWS::EC2::Instance", "Properties": {}}}}'
CS_BODY=$(CFN_TPL="$CS_TPL" python3 -c "import json,os; print(json.dumps({'name':'add-queue2','template':os.environ['CFN_TPL']}))")
call POST /api/v1/aws/cloudformation/stacks/$STACK/changesets "$TOKEN" "$CS_BODY"
expect 201 "create change set" "$CODE"
CS=$(echo "$BODY" | jget "['id']")
echo "$BODY" | python3 -c "import json,sys; d=json.load(sys.stdin); print('  changes:', [(c['logical_id'], c['action']) for c in d.get('changes',[])], '| executable:', d.get('executable'))" | tee -a "$LOG"
call POST /api/v1/aws/cloudformation/stacks/$STACK/changesets/$CS/execute "$TOKEN" -
expect 200 "execute change set" "$CODE"
echo "$BODY" | python3 -c "import json,sys; print('  status after execute:', json.load(sys.stdin).get('status'))" | tee -a "$LOG"
# A change set with only an unsupported change is not executable.
CSO_TPL='{"Resources": {"Queue": {"Type": "AWS::SQS::Queue", "Properties": {"QueueName": "cfn-orders-'$RUN'"}}, "Topic": {"Type": "AWS::SNS::Topic", "Properties": {"TopicName": "cfn-events-'$RUN'"}}, "Fn": {"Type": "AWS::Lambda::Function", "Properties": {"FunctionName": "cfn-fn-'$RUN'", "Runtime": "python3", "Code": {"ZipFile": "import json;print(json.dumps({\"ok\":True}))"}}}, "Queue2": {"Type": "AWS::SQS::Queue", "Properties": {"QueueName": "cfn-orders2-'$RUN'"}}, "Box": {"Type": "AWS::EC2::Instance", "Properties": {}}}}'
CSO_BODY=$(CFN_TPL="$CSO_TPL" python3 -c "import json,os; print(json.dumps({'name':'only-box','template':os.environ['CFN_TPL']}))")
call POST /api/v1/aws/cloudformation/stacks/$STACK/changesets "$TOKEN" "$CSO_BODY"
expect 201 "create unsupported-only change set" "$CODE"
CSO=$(echo "$BODY" | jget "['id']")
echo "$BODY" | python3 -c "import json,sys; print('  executable:', json.load(sys.stdin).get('executable'))" | tee -a "$LOG"
call POST /api/v1/aws/cloudformation/stacks/$STACK/changesets/$CSO/execute "$TOKEN" -
expect 409 "execute unsupported-only change set" "$CODE"
# S3 stack: no MinIO here, so CREATE_FAILED with rollback — not CREATE_COMPLETE.
call POST /api/v1/aws/cloudformation/stacks "$TOKEN" '{"name":"s3fail-'$RUN'","template":"{\"Resources\": {\"Bucket\": {\"Type\": \"AWS::S3::Bucket\", \"Properties\": {\"BucketName\": \"cfn-bucket-'$RUN'\"}}}}"}'
expect 201 "create S3 stack" "$CODE"
echo "$BODY" | python3 -c "import json,sys; d=json.load(sys.stdin); print('  status:', d.get('status'), '| failure_reason:', d.get('failure_reason'), '| rollback:', d.get('rollback_status'))" | tee -a "$LOG"
call DELETE /api/v1/aws/cloudformation/stacks/$STACK "$TOKEN" -
expect 200 "delete stack" "$CODE"
call GET /api/v1/aws/sqs/queues "$TOKEN" -
echo "$BODY" | python3 -c "import json,sys; print('  SQS queues still include cfn-orders:', any('cfn-orders' in q.get('name','') for q in json.load(sys.stdin).get('queues',[])))" | tee -a "$LOG"

step "14. cleanup: ECR images + repo, ECS service/cluster/task defs, CodeBuild projects, pipeline, app"
call DELETE /api/v1/aws/ecr/images/$IMG2 "$TOKEN" -
expect 200 "delete image v2" "$CODE"
call DELETE /api/v1/aws/ecr/repositories/$REPO "$TOKEN" -
expect 200 "delete repo" "$CODE"
call PUT /api/v1/aws/ecs/services/$PSVC "$TOKEN" '{"desired_count":0}'
sleep 2
call DELETE /api/v1/aws/ecs/services/$PSVC "$TOKEN" -
expect 200 "delete pipeline service" "$CODE"
call DELETE /api/v1/aws/ecs/clusters/$CLUSTER "$TOKEN" -
expect 200 "delete cluster" "$CODE"
call DELETE /api/v1/aws/ecs/task-definitions/$TD "$TOKEN" -
expect 200 "deregister task def v1" "$CODE"
call DELETE /api/v1/aws/ecs/task-definitions/$TD2 "$TOKEN" -
expect 200 "deregister task def v2" "$CODE"
call DELETE /api/v1/aws/ecs/task-definitions/$TD3 "$TOKEN" -
expect 200 "deregister task def v3" "$CODE"
call DELETE /api/v1/aws/ecs/task-definitions/$TDSLEEP "$TOKEN" -
expect 200 "deregister sleeper task def" "$CODE"
call DELETE /api/v1/aws/codebuild/projects/$PROJ "$TOKEN" -
expect 200 "delete good project" "$CODE"
call DELETE /api/v1/aws/codebuild/projects/$BADPROJ "$TOKEN" -
expect 200 "delete bad project" "$CODE"
call DELETE /api/v1/aws/codepipeline/pipelines/$PIPE "$TOKEN" -
expect 200 "delete pipeline" "$CODE"
call DELETE /api/v1/aws/codepipeline/pipelines/$TFPIPE "$TOKEN" -
expect 200 "delete tf pipeline" "$CODE"
call DELETE /api/v1/aws/codedeploy/applications/$APP "$TOKEN" -
expect 200 "delete application" "$CODE"

step "15. frontend: dev server + preview serve the new consoles"
curl -s -o /dev/null -w "vite dev /services/ecr: %{http_code}\n" http://localhost:5173/services/ecr | tee -a "$LOG"
curl -s -o /dev/null -w "vite preview /services/cloudformation: %{http_code}\n" http://localhost:8080/services/cloudformation | tee -a "$LOG"
curl -s http://localhost:5173/services/ecs | grep -o "CloudForge" | head -1 | tee -a "$LOG"

step "16. on-disk evidence: ECR blobs, CodeBuild logs, task logs"
find /tmp/cf-live/workdir/ecr /tmp/cf-live/workdir/codebuild /tmp/cf-live/workdir/ecs-tasks -type f 2>/dev/null | head -20 | tee -a "$LOG"
echo "  p4 tables in Postgres:" | tee -a "$LOG"
psql "postgres://cloudforge:cloudforge@localhost:5432/cloudforge?sslmode=disable" -t -A -c "SELECT count(*) FROM p4_ecr_repos; SELECT count(*) FROM p4_ecs_tasks; SELECT count(*) FROM p4_codebuild_builds; SELECT count(*) FROM p4_cfn_stacks;" 2>&1 | tee -a "$LOG"

echo "=== WALK RESULT: FAIL=$FAIL" | tee -a "$LOG"
exit $FAIL
