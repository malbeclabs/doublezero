#!/bin/bash
if [ -n "$1" ] ; then
	COMMIT_ID=$1
else
	COMMIT_ID=$(git rev-parse HEAD)
fi
for tag in $(git tag --contains $COMMIT_ID) ; do
	if [[ $tag == activator/* ]] ; then
		ACTIVATOR_VERSION=$(echo $tag | cut -f2 -d/)
	elif [[ $tag == controller/* ]] ; then
		CONTROLLER_VERSION=$(echo $tag | cut -f2 -d/)
	elif [[ $tag == agent/* ]] ; then
		AGENT_VERSION=$(echo $tag | cut -f2 -d/)
	elif [[ $tag == agent-telemetry/* ]] ; then
		AGENT_TELEMETRY_VERSION=$(echo $tag | cut -f2 -d/)
	elif [[ $tag == client/* ]] ; then
		CLIENT_VERSION=$(echo $tag | cut -f2 -d/)
	fi
done

echo ACTIVATOR_VERSION=$ACTIVATOR_VERSION
echo CONTROLLER_VERSION=$CONTROLLER_VERSION
echo AGENT_VERSION=$AGENT_VERSION
echo AGENT_TELEMETRY_VERSION=$AGENT_TELEMETRY_VERSION
echo CLIENT_VERSION=$CLIENT_VERSION
