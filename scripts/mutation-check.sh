#!/usr/bin/env bash
# Mutation check for VideoShareApp: verifies the Go suite catches seeded bugs.
#
# Run from PowerShell as:  bash scripts/mutation-check.sh
# (invoking the .sh directly from PowerShell does not execute it)
#
# For each mutation below, the script applies one small change to a throwaway
# copy of the working tree, builds, and runs the Go suite. A mutant is KILLED
# when the suite fails; SURVIVED means the tests did not notice the bug.
# Test output for every mutant is saved under test-results/mutation-check/.
#
# The real working tree is never modified. Workers default to 6; override with
# WORKERS=n scripts/mutation-check.sh ['filter-regex']

set -uo pipefail
cd "$(git rev-parse --show-toplevel)" || exit 1

WORKERS="${WORKERS:-6}"
if [[ ! "$WORKERS" =~ ^[1-9][0-9]*$ ]]; then
	echo "ERROR: WORKERS must be a positive integer" >&2
	exit 1
fi
start=$SECONDS
mkdir -p test-results/mutation-check || exit 1
logdir=$(mktemp -d "$PWD/test-results/mutation-check/$(date +%Y%m%d-%H%M%S)-XXXXXX") || exit 1
exec > >(tee "$logdir/console.log") 2>&1

work="$(mktemp -d "${TMPDIR:-/tmp}/vsmutation-XXXXXX")" || exit 1
ticker=""
pids=()
cleanup() {
	[[ -z "$ticker" ]] || kill "$ticker" 2>/dev/null || true
	local pid
	for pid in "${pids[@]}"; do
		kill "$pid" 2>/dev/null || true
	done
	rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

progress() {
	printf '[%s +%ss] %s\n' "$(date +%H:%M:%S)" "$((SECONDS - start))" "$*"
}

# Progress ticker so long runs visibly stay alive.
(
	while sleep 30; do
		completed=$(cat "$work"/status.* 2>/dev/null | wc -l)
		progress "still running, please wait ... ($completed mutants completed)"
	done
) &
ticker=$!

progress "copying source tree (excluding dependencies, results, secrets, and production data)"
mkdir "$work/base" || exit 1
tar cf - \
	--exclude=.git \
	--exclude=node_modules \
	--exclude=test-results \
	--exclude=playwright-report \
	--exclude=data \
	--exclude=Videos \
	--exclude='static/uploads' \
	--exclude='*.exe' \
	--exclude='.env*' \
	--exclude='*.env' \
	. | (cd "$work/base" && tar xf -) || exit 1

# Entry format: file ~ sed expression ~ description
mutations=(
	"internal/app/login_limiter.go~s/bucket.attempts >= loginLimitAttempts/bucket.attempts >= loginLimitAttempts+1000/~rate limiter never blocks"
	"internal/app/helpers.go~s/or datetime(created_at) > datetime('now')/or 0/~future sessions are not purged"
	"internal/app/helpers.go~s/and datetime(s.created_at) > datetime('now', ? || ' seconds')/and 1 = 1/~expired sessions still authenticate"
	"internal/app/request_forms.go~s/defaultFormBodyLimit int64 = 1 << 20/defaultFormBodyLimit int64 = 1 << 40/~ordinary form body limit disabled"
	"internal/app/handlers_media.go~s/isPublic == 0)/isPublic == 9)/~private media no longer denied"
	"internal/app/handlers_channel.go~s/if ch.IsAdminLocked [&][&] (user == nil || !user.IsAdmin) {/if false {/~locked channel page visible to all"
	"internal/app/handlers_channel.go~s/if channelAdminLocked != 0 [&][&] !user.IsAdmin {/if false {/~locked channel actions allowed"
	"internal/app/handlers_channel.go~s/if removeAvatar [&][&] avatarData != \"\" {/if false {/~avatar remove+replace conflict accepted"
	"internal/app/handlers_video.go~s/utf8.RuneCountInString(content) > 4000/utf8.RuneCountInString(content) > 400000/~comment length limit disabled"
	"internal/app/handlers_auth.go~s/len(password) > 72 ||/len(password) > 7200 ||/~password byte limit disabled"
	"internal/app/handlers_video.go~s/if channelOwnerID != user.ID {/if false {/~upload to foreign channel allowed"
	"internal/app/handlers_video.go~s/v.is_public = 1 or v.user_id = ?/1 = 1 or v.user_id = ?/~private videos visible to everyone"
	"internal/app/thumbnails.go~s/config.Width > maxThumbnailDimension/config.Width > maxThumbnailDimension+4096/~thumbnail dimension limit disabled"
	"internal/app/helpers.go~s/u.location, u.is_admin/u.location, 1/~every session resolves as admin"
	"internal/app/handlers_media.go~s/user != nil && user.ID == channelOwnerID/user != nil/~any logged-in user counts as media owner"
	"internal/app/handlers_media.go~s/channelAdminLocked == 1 ||/channelAdminLocked == 9 ||/~media auth ignores channel lock"
	"internal/app/handlers_video.go~s/if user != nil && user.IsAdmin {/if user != nil {/~watch admin visibility granted to all users"
	"internal/app/handlers_video.go~s/if !isOwner && !user.IsAdmin {/if !isOwner \&\& false {/~edit video open to non-owners"
	"internal/app/handlers_video.go~s/user.ID != commentOwnerID && user.ID != channelOwnerID && !user.IsAdmin {/user.ID != commentOwnerID \&\& user.ID != channelOwnerID \&\& false {/~any user can delete any comment"
	"internal/app/handlers_video.go~s/if !user.IsAdmin &&/if false \&\&/~comments allowed on locked/private videos"
	"internal/app/handlers_video.go~445s/if user.IsAdmin {/if true {/~video admin lock toggle granted to owners"
	"internal/app/handlers_channel.go~s/if !isOwner && !user.IsAdmin {/if !isOwner \&\& false {/~edit channel open to non-owners"
	"internal/app/handlers_channel.go~s/(user == nil || !user.IsAdmin)/(user == nil || false)/~locked channel page visible to logged-in users"
	"internal/app/handlers_channel.go~s/if user != nil && user.IsAdmin {/if user != nil {/~channel page admin visibility granted to all users"
	"internal/app/handlers_channel.go~346s/if user.IsAdmin {/if true {/~channel admin lock toggle granted to owners"
	"internal/app/loaders.go~s/if user == nil || !user.IsAdmin {/if user == nil || false {/~discovery shows locked channels to logged-in users"
	"internal/app/loaders.go~s/where c.user_id = ?/where c.user_id = ? or 1 = 1/~dashboard lists every user's videos"
	"internal/app/loaders.go~s/from channels where user_id = ? order by name/from channels where user_id = ? or 1 = 1 order by name/~dashboard lists every user's channels"
)

filter="${1:-}"
selected=()
for entry in "${mutations[@]}"; do
	desc="${entry##*~}"
	if [[ -z "$filter" || "$desc" =~ $filter ]]; then
		selected+=("$entry")
	fi
done

total=${#selected[@]}
progress "${total} mutants, ${WORKERS} workers, package under test: VideoShareApp/internal/app"
progress "logs: $logdir"
if ((total == 0)); then
	progress "ERROR: no mutants match filter: $filter"
	exit 1
fi

# An unmodified baseline must pass before a failing mutant can mean anything.
progress "BASELINE build ./..."
if ! (cd "$work/base" && go build ./... >"$logdir/baseline.log" 2>&1); then
	progress "ERROR: baseline build failed; see $logdir/baseline.log"
	cat "$logdir/baseline.log"
	exit 1
fi
progress "BASELINE test ./internal/app"
if ! (cd "$work/base" && go test -count=1 -timeout=2m ./internal/app >>"$logdir/baseline.log" 2>&1); then
	progress "ERROR: baseline tests failed; mutation results would be invalid"
	cat "$logdir/baseline.log"
	exit 1
fi
progress "BASELINE passed"

worker_status() {
	local w=$1
	local dir="$work/worker-$w"
	local i entry file sedExpr desc slug log mutant_start result failing
	for ((i = w; i < total; i += WORKERS)); do
		entry="${selected[$i]}"
		IFS='~' read -r file sedExpr desc <<<"$entry"
		slug=$(printf '%s' "$desc" | tr ' /' '__')
		log="$logdir/$((i + 1))-$slug.txt"
		mutant_start=$SECONDS
		progress "START worker=$((w + 1)) mutant=$((i + 1))/$total $file: $desc"

		failing=""
		if ! sed "$sedExpr" "$work/base/$file" >"$dir/$file" 2>"$log"; then
			result=ERROR
		elif cmp -s "$dir/$file" "$work/base/$file"; then
			result=NOOP
			echo "Mutation matched nothing; check the entry." >>"$log"
		elif ! (cd "$dir" && go build ./... >>"$log" 2>&1); then
			result=INVALID
		elif (cd "$dir" && go test -count=1 -timeout=2m ./internal/app >>"$log" 2>&1); then
			result=SURVIVED
		elif grep -q '^--- FAIL: ' "$log"; then
			result=KILLED
			failing=$(sed -n 's/^--- FAIL: / /p' "$log" | tr '\n' ' ')
		else
			result=ERROR
		fi
		printf '%s %s: %s\n' "$result" "$file" "$desc" >>"$work/status.$w"
		progress "$result worker=$((w + 1)) mutant=$((i + 1))/$total ($((SECONDS - mutant_start))s) $file: $desc${failing:+ (failing:$failing)}"
		if [[ "$result" != KILLED ]]; then
			progress "  log: $log"
		fi
		# Every worker has its own tree; only one mutation is active in it.
		cp "$work/base/$file" "$dir/$file" || return 1
	done
	progress "worker=$((w + 1)) finished"
}

active=$WORKERS
((active <= total)) || active=$total
for ((w = 0; w < active; w++)); do
	progress "preparing isolated source tree for worker=$((w + 1))"
	mkdir "$work/worker-$w" || exit 1
	cp -R "$work/base/." "$work/worker-$w/" || exit 1
	: >"$work/status.$w"
done
for ((w = 0; w < active; w++)); do
	worker_status "$w" &
	pids+=($!)
done
worker_errors=0
for pid in "${pids[@]}"; do
	wait "$pid" || worker_errors=$((worker_errors + 1))
done
kill "$ticker" 2>/dev/null || true
wait "$ticker" 2>/dev/null || true
ticker=""
pids=()

killed=0
survived=0
other=0
for ((w = 0; w < active; w++)); do
	while read -r status desc; do
		case "$status" in
		KILLED) killed=$((killed + 1)) ;;
		SURVIVED) survived=$((survived + 1)) ;;
		*) other=$((other + 1)) ;;
		esac
	done <"$work/status.$w"
done

elapsed=$((SECONDS - start))
progress "DONE in ${elapsed}s: $killed killed, $survived survived, $other invalid/noop/error, of $total mutants"
progress "console transcript: $logdir/console.log"
if ((survived > 0 || other > 0 || worker_errors > 0 || killed != total)); then
	progress "NOT KILLED (coverage gaps, script errors, or missing results; worker errors=$worker_errors):"
	for ((w = 0; w < active; w++)); do
		grep -v '^KILLED' "$work/status.$w" || true
	done
	exit 1
fi
