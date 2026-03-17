#!/usr/bin/env bash
set -euo pipefail

variants=(
  ""
  "_BC"
  "_BC_CC"
  "_KV"
)

pretty_diff() {
  local secs=$1
  local sign=""
  if (( secs < 0 )); then
    sign="-"
    secs=$(( -secs ))
  fi
  local minutes=$(( secs / 60 ))
  local seconds=$(( secs % 60 ))
  if (( minutes > 0 )); then
    printf '%s%dm %ds\n' "$sign" "$minutes" "$seconds"
  else
    printf '%s%ds\n' "$sign" "$seconds"
  fi
}

# read APPS into an array
apps=()
while IFS= read -r line || [[ -n $line ]]; do
  apps+=("$line")
done <<< "$APPS"

# build all variants
app_variants=()
for base in "${apps[@]}"; do
  app_variants+=("$base")
  for v in "${variants[@]}"; do
    [[ -z "$v" ]] && continue
    app_variants+=("${base}${v}")
  done
done

# duration in seconds from START/END var names
duration_seconds() {
  local start_name=$1
  local end_name=$2
  echo $(( ${!end_name} - ${!start_name} ))
}

# percent change with one decimal using integer math:
# returns string like "+12.3%" or "-4.0%" or "0.0%"
percent_change_one_decimal() {
  local base_secs=$1
  local var_secs=$2

  if (( base_secs == 0 )); then
    printf 'N/A'
    return
  fi

  local diff=$(( base_secs - var_secs ))       # positive = improvement
  local scaled=$(( (diff * 1000) / base_secs )) # tenths of percent (integer)
  local sign=""
  if (( scaled > 0 )); then
    sign="+"
  elif (( scaled < 0 )); then
    sign="-"
    scaled=$(( -scaled ))
  fi
  local int_part=$(( scaled / 10 ))
  local frac=$(( scaled % 10 ))
  printf '%s%d.%d%%' "$sign" "$int_part" "$frac"
}

for app in "${app_variants[@]}"; do
  start="${app}_START"
  end="${app}_END"
  # skip if missing
  [[ -z "${!start-}" ]] && continue
  [[ -z "${!end-}" ]] && continue

  dur=$(duration_seconds "$start" "$end")
  printf 'Duration for %s: ' "$app"
  pretty_diff "$dur"

  # determine base name (before first underscore)
  base_name="$app"
  if [[ "$app" == *"_"* ]]; then
    base_name="${app%%_*}"
  fi

  # skip comparison when this is the base itself
  if [[ "$app" == "$base_name" ]]; then
    # nothing to compare to
    continue
  fi

  base_start="${base_name}_START"
  base_end="${base_name}_END"

  if [[ -z "${!base_start-}" || -z "${!base_end-}" ]]; then
    printf '  Base (%s) missing START/END — cannot compute percent change.\n' "$base_name"
    continue
  fi

  base_dur=$(duration_seconds "$base_start" "$base_end")
  pc=$(percent_change_one_decimal "$base_dur" "$dur")

  if [[ "$pc" == "N/A" ]]; then
    printf '  Percent change vs %s: N/A (base duration is zero)\n' "$base_name"
    continue
  fi

  if [[ "$pc" == "+0.0%" || "$pc" == "0.0%" ]]; then
    printf '  Percent change vs %s: 0.0%% (equal)\n' "$base_name"
  else
    if (( dur < base_dur )); then
      label="improved"
    elif (( dur > base_dur )); then
      label="slower"
    else
      label="equal"
    fi
    printf '  Percent change vs %s: %s (%s)\n' "$base_name" "$pc" "$label"
  fi
done
