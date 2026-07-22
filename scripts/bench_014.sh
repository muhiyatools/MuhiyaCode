#!/usr/bin/env sh
# Reproducible POSIX runner for feature 014. Live execution is refused unless
# --allow-paid is present. Requires Node.js >=16 for strict JSON/file handling.
set -eu

usage() {
  cat <<'EOF'
Usage: scripts/bench_014.sh --binary PATH --model ID --transport NAME \
  --effort low|medium|high|max --permission normal|auto-accept \
  --fixture FILE --output DIR [--repeat N] \
  [--mode baseline|observe|balanced|aggressive] [--no-mcp] --allow-paid
EOF
}

BINARY= MODEL= TRANSPORT= EFFORT= PERMISSION= FIXTURE= OUTPUT=
REPEAT=2 MODE=baseline ALLOW_PAID=0 KEEP_WORKSPACES=0 NO_MCP=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --binary) BINARY=$2; shift 2 ;;
    --model) MODEL=$2; shift 2 ;;
    --transport) TRANSPORT=$2; shift 2 ;;
    --effort) EFFORT=$2; shift 2 ;;
    --permission) PERMISSION=$2; shift 2 ;;
    --fixture) FIXTURE=$2; shift 2 ;;
    --output) OUTPUT=$2; shift 2 ;;
    --repeat) REPEAT=$2; shift 2 ;;
    --mode) MODE=$2; shift 2 ;;
    --allow-paid) ALLOW_PAID=1; shift ;;
    --keep-workspaces) KEEP_WORKSPACES=1; shift ;;
    --no-mcp) NO_MCP=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 1 ;;
  esac
done

[ -n "$BINARY" ] && [ -n "$MODEL" ] && [ -n "$TRANSPORT" ] && [ -n "$EFFORT" ] && \
  [ -n "$PERMISSION" ] && [ -n "$FIXTURE" ] && [ -n "$OUTPUT" ] || { usage >&2; exit 1; }
case "$EFFORT" in low|medium|high|max) ;; *) echo "invalid effort" >&2; exit 1;; esac
case "$PERMISSION" in normal|auto-accept) ;; *) echo "invalid permission" >&2; exit 1;; esac
case "$MODE" in baseline|observe|balanced|aggressive) ;; *) echo "invalid mode" >&2; exit 1;; esac
case "$REPEAT" in ''|*[!0-9]*) echo "repeat must be an integer" >&2; exit 1;; esac
[ "$REPEAT" -ge 1 ] && [ "$REPEAT" -le 100 ] || { echo "repeat must be 1..100" >&2; exit 1; }
[ "$ALLOW_PAID" -eq 1 ] || { echo "live provider runs consume credits; pass --allow-paid explicitly" >&2; exit 1; }
command -v node >/dev/null 2>&1 || { echo "node is required" >&2; exit 1; }

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
abspath() { case "$1" in /*) printf '%s\n' "$1";; *) printf '%s/%s\n' "$REPO_ROOT" "$1";; esac; }
BINARY=$(abspath "$BINARY")
FIXTURE=$(abspath "$FIXTURE")
OUTPUT=$(abspath "$OUTPUT")
[ -f "$BINARY" ] || { echo "binary not found: $BINARY" >&2; exit 1; }
[ -f "$FIXTURE" ] || { echo "fixture not found: $FIXTURE" >&2; exit 1; }

TASK_COUNT=$(node -e 'const s=require(process.argv[1]); if(s.schema_version!==1||!s.suite||!Array.isArray(s.tasks)||!s.tasks.length)process.exit(2); for(const t of s.tasks)if(!t.id||!t.prompt||!t.rubric||!t.rubric.checks||!t.rubric.checks.length)process.exit(3); process.stdout.write(String(s.tasks.length))' "$FIXTURE")
mkdir -p "$OUTPUT"
STAMP=$(date -u +%Y%m%d-%H%M%S)
RUN_ID="014-$MODE-$STAMP"
RUN_DIR="$OUTPUT/$RUN_ID"
mkdir -p "$RUN_DIR"
SCRATCH=$(mktemp -d "${TMPDIR:-/tmp}/muhiya-bench-014.XXXXXX")
SOURCE_HOME=${MUHIYA_HOME:-${HOME}/.muhiya}
BENCH_HOME="$SCRATCH/home"
mkdir -p "$BENCH_HOME"
if [ -d "$SOURCE_HOME" ]; then cp -R "$SOURCE_HOME"/. "$BENCH_HOME"/; fi
PREVIOUS_HOME=${MUHIYA_HOME-}
export MUHIYA_HOME=$BENCH_HOME MUHIYA_BENCH_JSON=1 MUHIYA_BENCH_TRANSPORT=$TRANSPORT
if [ "$MODE" = baseline ]; then export MUHIYA_TOKEN_ECONOMY_MODE=off; else export MUHIYA_TOKEN_ECONOMY_MODE=$MODE; fi

cleanup() {
  unset MUHIYA_BENCH_JSON MUHIYA_BENCH_TRANSPORT MUHIYA_TOKEN_ECONOMY_MODE
  if [ -n "$PREVIOUS_HOME" ]; then export MUHIYA_HOME=$PREVIOUS_HOME; else unset MUHIYA_HOME; fi
  [ "$KEEP_WORKSPACES" -eq 1 ] || rm -rf -- "$SCRATCH"
}
trap cleanup EXIT HUP INT TERM

"$BINARY" config set model "$MODEL" >/dev/null
"$BINARY" config set effort "$EFFORT" >/dev/null
"$BINARY" config set permissionMode "$PERMISSION" >/dev/null

TASK_IDS=$(node -e 'for(const t of require(process.argv[1]).tasks) console.log(t.id)' "$FIXTURE")
RECORDS_DIR="$SCRATCH/records"
mkdir -p "$RECORDS_DIR"
INVALID=0
INDEX=0
repeat_index=1
while [ "$repeat_index" -le "$REPEAT" ]; do
  for task_id in $TASK_IDS; do
    INDEX=$((INDEX+1))
    WORKSPACE="$SCRATCH/repeat-$repeat_index/$task_id"
    mkdir -p "$WORKSPACE"
    FIXTURE_PATH=$FIXTURE TASK_ID=$task_id WORKSPACE_PATH=$WORKSPACE REPO_PATH=$REPO_ROOT node <<'NODE'
const fs=require('fs'),path=require('path'),crypto=require('crypto');
const suite=JSON.parse(fs.readFileSync(process.env.FIXTURE_PATH,'utf8'));
const task=suite.tasks.find(t=>t.id===process.env.TASK_ID); if(!task) process.exit(2);
let reset=task.reset?suite.reset_scripts?.[task.reset]:task.workspace?.reset; if(!reset) process.exit(3);
const ws=process.env.WORKSPACE_PATH;
if(reset.strategy==='copy') {
  const src=path.isAbsolute(reset.source)?reset.source:path.join(process.env.REPO_PATH,reset.source);
  fs.cpSync(src,ws,{recursive:true,force:true});
} else if(reset.strategy==='materialize') {
  for(const [name,body] of Object.entries(reset.files||{})){const p=path.join(ws,name);fs.mkdirSync(path.dirname(p),{recursive:true});fs.writeFileSync(p,body,'utf8');}
} else if(reset.strategy!=='empty') process.exit(4);
function walk(dir,out={}){for(const e of fs.readdirSync(dir,{withFileTypes:true})){const p=path.join(dir,e.name);if(e.isDirectory())walk(p,out);else out[path.relative(ws,p).split(path.sep).join('/')]=crypto.createHash('sha256').update(fs.readFileSync(p)).digest('hex')}return out}
fs.writeFileSync(path.join(ws,'.bench-before.json'),JSON.stringify(walk(ws)));
NODE
    PROMPT=$(FIXTURE_PATH=$FIXTURE TASK_ID=$task_id node -e 'const s=require(process.env.FIXTURE_PATH),t=s.tasks.find(x=>x.id===process.env.TASK_ID);process.stdout.write(t.prompt)')
    STDOUT="$RUN_DIR/$repeat_index-$task_id-stdout.txt"
    STDERR="$RUN_DIR/$repeat_index-$task_id-stderr.txt"
    START=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    set +e
    if [ "$NO_MCP" -eq 1 ]; then "$BINARY" --no-mcp --cwd "$WORKSPACE" --new --print "$PROMPT" >"$STDOUT" 2>"$STDERR"
    else "$BINARY" --cwd "$WORKSPACE" --new --print "$PROMPT" >"$STDOUT" 2>"$STDERR"; fi
    EXIT_CODE=$?
    set -e
    RECORD="$RECORDS_DIR/$(printf '%04d' "$INDEX").json"
    FIXTURE_PATH=$FIXTURE TASK_ID=$task_id WORKSPACE_PATH=$WORKSPACE STDOUT_PATH=$STDOUT STDERR_PATH=$STDERR \
      EXIT_CODE=$EXIT_CODE REPEAT_INDEX=$repeat_index STARTED_AT=$START RECORD_PATH=$RECORD node <<'NODE'
const fs=require('fs'),path=require('path'),cp=require('child_process'),crypto=require('crypto');
const suite=JSON.parse(fs.readFileSync(process.env.FIXTURE_PATH,'utf8')),task=suite.tasks.find(t=>t.id===process.env.TASK_ID),ws=process.env.WORKSPACE_PATH;
const before=JSON.parse(fs.readFileSync(path.join(ws,'.bench-before.json'),'utf8'));fs.unlinkSync(path.join(ws,'.bench-before.json'));
function walk(dir,out={}){for(const e of fs.readdirSync(dir,{withFileTypes:true})){const p=path.join(dir,e.name);if(e.isDirectory())walk(p,out);else out[path.relative(ws,p).split(path.sep).join('/')]=crypto.createHash('sha256').update(fs.readFileSync(p)).digest('hex')}return out}
const after=walk(ws),changed=[...new Set([...Object.keys(before),...Object.keys(after)])].filter(k=>before[k]!==after[k]).sort();
const exists=p=>fs.existsSync(path.join(ws,p)),read=p=>fs.readFileSync(path.join(ws,p),'utf8');
function globAbsent(globs){const names=Object.keys(after);return globs.every(g=>{const re=new RegExp('^'+g.replace(/[.+^${}()|[\]\\]/g,'\\$&').replace(/\*\*/g,'.*').replace(/\*/g,'[^/]*')+'$');return !names.some(n=>re.test(n))})}
function check(c){try{switch(c.kind){case'file_exists':return exists(c.path);case'file_exists_any':return c.paths.some(exists);case'file_contains':return read(c.path).includes(c.text);case'file_not_contains':return !read(c.path).includes(c.text);case'source_regex':return new RegExp(c.pattern).test(c.paths.filter(exists).map(read).join('\n'));case'glob_absent':return globAbsent(c.globs);case'max_changed_files':return changed.length<=c.value;case'command':return cp.spawnSync(c.argv[0],c.argv.slice(1),{cwd:ws,stdio:'ignore'}).status===0;case'html_selector_count':{const h=read(c.path);let n=0;for(const s of c.selector.split(',').map(x=>x.trim())){if(s==='[data-cell]')n+=(h.match(/\bdata-cell\b/gi)||[]).length;else if(s==='.cell')n+=(h.match(/class\s*=\s*["'][^"']*\bcell\b/gi)||[]).length;else if(s==='button')n+=(h.match(/<button\b/gi)||[]).length;else if(s==='header.site-header')n+=(h.match(/<header\b[^>]*class\s*=\s*["'][^"']*\bsite-header\b/gi)||[]).length;else throw Error('unsupported selector')}return n>=c.minimum}default:throw Error('unsupported kind')}}catch{return false}}
const rubric=task.rubric.checks.map(c=>({check_id:c.id||c.kind,kind:c.kind,passed:check(c)}));
let summary=null;for(const line of fs.readFileSync(process.env.STDOUT_PATH,'utf8').split(/\r?\n/)){if(line.includes('"muhiya_bench"'))try{summary=JSON.parse(line).muhiya_bench}catch{}}
const usageAvailable=!!(summary&&summary.usage&&summary.usage.reported),invalid=[];if(+process.env.EXIT_CODE!==0)invalid.push('agent_exit_'+process.env.EXIT_CODE);if(!summary)invalid.push('missing_machine_summary');if(!usageAvailable)invalid.push('missing_provider_usage');if(!rubric.length)invalid.push('missing_rubric');if(rubric.some(r=>!r.passed))invalid.push('rubric_failed');
fs.writeFileSync(process.env.RECORD_PATH,JSON.stringify({task_id:task.id,category:task.category,declared_task_class:task.task_class,repeat:+process.env.REPEAT_INDEX,started_at:process.env.STARTED_AT,exit_code:+process.env.EXIT_CODE,summary,rubric,changed_paths:changed,valid:!invalid.length,invalid_reasons:invalid,usage_available:usageAvailable,stdout:path.basename(process.env.STDOUT_PATH),stderr:path.basename(process.env.STDERR_PATH)}));
NODE
    if ! node -e 'process.exit(require(process.argv[1]).valid?0:1)' "$RECORD"; then INVALID=$((INVALID+1)); fi
  done
  repeat_index=$((repeat_index+1))
done

GIT_COMMIT=$(git -C "$REPO_ROOT" rev-parse HEAD)
RUN_ID=$RUN_ID RUN_DIR=$RUN_DIR RECORDS_DIR=$RECORDS_DIR FIXTURE_PATH=$FIXTURE BINARY_PATH=$BINARY \
MODEL_NAME=$MODEL TRANSPORT_NAME=$TRANSPORT EFFORT_NAME=$EFFORT PERMISSION_NAME=$PERMISSION MODE_NAME=$MODE \
REPEAT_COUNT=$REPEAT EXPECTED_RECORDS=$((TASK_COUNT*REPEAT)) GIT_COMMIT=$GIT_COMMIT NO_MCP_VALUE=$NO_MCP node <<'NODE'
const fs=require('fs'),path=require('path'),crypto=require('crypto'),hash=p=>crypto.createHash('sha256').update(fs.readFileSync(p)).digest('hex');
const records=fs.readdirSync(process.env.RECORDS_DIR).sort().map(f=>JSON.parse(fs.readFileSync(path.join(process.env.RECORDS_DIR,f),'utf8'))),suite=require(process.env.FIXTURE_PATH);
const out={schema_version:1,run_id:process.env.RUN_ID,suite:suite.suite,fixture_schema_version:suite.schema_version,fixture_path:process.env.FIXTURE_PATH,fixture_sha256:hash(process.env.FIXTURE_PATH),binary_path:process.env.BINARY_PATH,binary_sha256:hash(process.env.BINARY_PATH),git_commit:process.env.GIT_COMMIT,model:process.env.MODEL_NAME,transport:process.env.TRANSPORT_NAME,effort:process.env.EFFORT_NAME,permission:process.env.PERMISSION_NAME,mode:process.env.MODE_NAME,repeat:+process.env.REPEAT_COUNT,no_mcp:process.env.NO_MCP_VALUE==='1',expected_records:+process.env.EXPECTED_RECORDS,records};
fs.writeFileSync(path.join(process.env.RUN_DIR,'run.json'),JSON.stringify(out,null,2));
NODE
echo "run=$RUN_ID records=$((TASK_COUNT*REPEAT)) invalid=$INVALID output=$RUN_DIR/run.json"
[ "$INVALID" -eq 0 ] || exit 2
