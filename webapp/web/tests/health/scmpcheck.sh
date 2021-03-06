#!/bin/bash
# test will fail for non-zero exit and/or bytes in stderr

# error exit function
error_exit()
{
    echo "$1" 1>&2
    exit 1
}

sdaddress=$(echo $2)
echo "sciond address: $sdaddress"

# get local IP
ip=$(hostname -I | cut -d" " -f1)
echo "IP found: $ip"

topologyFile=$3
# get remote addresses from interfaces, return paired list
dsts=($(cat $topologyFile | python3 -c "import sys, json
brs = json.load(sys.stdin)['border_routers']
for b in brs:
    for i in brs[b]['interfaces']:
        print(brs[b]['interfaces'][i]['isd_as'])
        print((brs[b]['interfaces'][i]['underlay']['remote']).split(':')[0])"))
if [ -z "$dsts" ]; then
    error_exit "No interface addresses in $topologyFile."
fi

# test scmp echo on each interface
for ((i=0; i<${#dsts[@]}; i+=2))
do
    # if no response under default scmp ping timeout consider connection failed
    ia_dst="${dsts[i]}"
    ip_dst="${dsts[i+1]}"
    cmd="scion ping -c 1 --sciond $sdaddress --timeout 5s $ia_dst,[$ip_dst]"
    echo "Running: $cmd"
    recv=$($cmd | grep -E -o '[0-9]+ received' | cut -f1 -d' ')
    if [ "$recv" != "1" ]; then
        error_exit "SCMP echo failed from $ia_dst,[$ip_dst]."
    else
        echo "SCMP echo succeeded."
    fi
done
