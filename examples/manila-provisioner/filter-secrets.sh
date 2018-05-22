#!/bin/bash

id_key=false

while IFS= read -r line; do
	if [[ "$line" == *\"\" ]]; then continue; fi

	key=$(echo $line | cut -d ':' -f 1)

	if [[ "$key" == *ID ]]; then
		id_key=true
		echo "$line"
	elif [[ "$key" == *Name ]]; then
		if ! $id_key; then
			echo "$line"
		fi
		id_key=false
	else
		echo "$line"
	fi
done

