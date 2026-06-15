#!/bin/bash
PID=$1
count=$2
div=${count}
count=$((${count}+1))
echo "count :$count"

array=($(top -b -d 2 -n $count -p $PID | awk '$1+0>0 {printf "%f\n", $9}'))
echo "${array[@]}"

avg=0
for((i=1;i<$count;i++)); do
  avg=$(echo "${avg}+${array[i]}" | bc)
done
echo "sum :$avg"
avg=$(echo "scale=2; ${avg} / ${div}" | bc)
echo "avg :$avg"