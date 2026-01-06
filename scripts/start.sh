#!/bin/bash

export CF_API_KEY=
export CF_API_EMAIL=
export CF_ZONE_NAME=

wgs=("wg02")

for wg in ${wgs[@]}
do
	echo $wg
	export WG=$wg
	/tmp/stunmesh-go
	sleep 10s
	/tmp/stunmesh-go
done
