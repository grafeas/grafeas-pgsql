#!/bin/sh

HOST=$(printf "%s\n" "$1"| cut -d : -f 1)
PORT=$(printf "%s\n" "$1"| cut -d : -f 2)
TIMEOUT="$2"

if [ "$HOST" = "" ]; then exit 1; fi
if [ "$PORT" = "" ]; then exit 1; fi
if [ "$TIMEOUT" = "" ]; then exit 1; fi

for i in `seq $TIMEOUT` ; do
  nc -z "$HOST" "$PORT" 
  out=$?
  if [ $out -eq 0 ]; then
    exit 0
  fi
  sleep 1
done
exit 1
