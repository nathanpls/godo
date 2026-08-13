#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
install_dir=${HOME:?HOME is not set}/.local/bin
temporary=$(mktemp "${TMPDIR:-/tmp}/godo.XXXXXX")
trap 'rm -f "$temporary"' EXIT HUP INT TERM

mkdir -p "$install_dir"
go build -o "$temporary" "$script_dir/godo"
chmod 755 "$temporary"
mv "$temporary" "$install_dir/godo"
trap - EXIT HUP INT TERM

printf 'Installed godo to %s\n' "$install_dir/godo"
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) printf 'Add %s to PATH to run godo from your terminal.\n' "$install_dir" ;;
esac
