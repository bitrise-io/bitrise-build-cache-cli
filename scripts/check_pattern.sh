#!/bin/bash
set -eo pipefail

# Function to display usage information
usage() {
    echo "Usage: $0 <file_path> <pattern1> <pattern2> [<pattern3> ...]"
    exit 1
}

# Check if at least two arguments are provided (file path and one pattern)
if [ $# -lt 2 ]; then
    usage
fi

# Assign the first argument to file_to_check and shift it from the argument list
file_to_check="$1"
shift

# Check if the file exists
if [ ! -f "$file_to_check" ]; then
    echo "Error: File '$file_to_check' not found."
    exit 1
fi

# Function to check if a pattern exists in the file
check_pattern() {
    local file=$1
    local pattern=$2
    if grep -iE -- "$pattern" "$file"; then
        return 0 # Pattern found
    else
        return 1 # Pattern not found
    fi
}

# Iterate over each pattern and check it
for pattern in "$@"; do
    if ! check_pattern "$file_to_check" "$pattern"; then
        echo "Pattern not found: $pattern"
        exit 1
    fi
done

echo "All patterns are present in the file."
