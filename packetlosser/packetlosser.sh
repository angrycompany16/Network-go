# Credit of claude.ai

while getopts "fr:" flag; do
 case $flag in
   f)
    sudo tc qdisc del dev lo root
   ;;
   r) 
    sudo tc qdisc del dev lo root

    rate=$OPTARG
    sudo tc qdisc add dev lo root netem loss $rate%
   ;;
   \?)
   # Handle invalid options
   ;;
 esac
done