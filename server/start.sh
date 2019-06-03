#!/bin/sh

config='config.yaml'
timeout=10

if [ -f $config ] 
then
  db=`grep -Eo "host:\s*\"(.+:[0-9]+)\"" $config | cut -d \" -f2`
  echo "Database: $db"

  $(./wait.sh $db $timeout)
  up=$?
  echo "Database up=0? $up"

  if [ $up -eq 0 ] ; then
    $(./grafeas-server --config $config)
  else
    echo "Database is down, exiting" 1>&2
    exit 1
  fi
else
  echo "No config file is specified, exiting" 1>&2
  exit 1
fi

echo "Done"
