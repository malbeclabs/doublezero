#!/bin/bash

killall solana-test-validator
killall doublezero-activator
killall solana

kill -9 $(ps -ef | grep activator | grep -v grep | awk '{print $2}')
