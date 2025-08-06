#!/bin/bash

#solana config set --url devnet
cargo build

### Init
./target/debug/doublezero init 

### Config
./target/debug/doublezero global-config set --local-asn 65100 --remote-asn 65001 --Link-Link-block 172.16.0.0/16 --device-Link-block 169.254.0.0/16

### Locations
./target/debug/doublezero location create --code la --name "Los Angeles" --country US --lat 34.049641274076464 --lng -118.25939642499903
./target/debug/doublezero location create --code ny --name "New York" --country US --lat 40.780297071772125 --lng -74.07203003496925
./target/debug/doublezero location create --code ld --name "London" --country UK --lat 51.513999803939384 --lng -0.12014764843092213
./target/debug/doublezero location create --code fr --name "Frankfurt" --country DE --lat 50.1215356432098 --lng 8.642047117175098
./target/debug/doublezero location create --code sg --name "Singapore" --country SG --lat 1.2807150707390342 --lng 103.85507136144396
./target/debug/doublezero location create --code ty --name "Tokyo" --country JP --lat 35.66875144228767 --lng 139.76565267564501
./target/debug/doublezero location create --code pi --name "Pittsburgh" --country US --lat 40.45119259881935 --lng -80.00498215509094
./target/debug/doublezero location create --code ams --name "Amsterdam" --country US --lat 52.30085793004002 --lng 4.942241140085309

### Exchanges
./target/debug/doublezero exchange create --code xla --name "Los Angeles" --lat 34.049641274076464 --lng -118.25939642499903
./target/debug/doublezero exchange create --code xny --name "New York" --lat 40.780297071772125 --lng -74.07203003496925
./target/debug/doublezero exchange create --code xld --name "London" --lat 51.513999803939384 --lng -0.12014764843092213
./target/debug/doublezero exchange create --code xfr --name "Frankfurt" --lat 50.1215356432098 --lng 8.642047117175098
./target/debug/doublezero exchange create --code xsg --name "Singapore" --lat 1.2807150707390342 --lng 103.85507136144396
./target/debug/doublezero exchange create --code xty --name "Tokyo" --lat 35.66875144228767 --lng 139.76565267564501
./target/debug/doublezero exchange create --code xpi --name "Pittsburgh" --lat 40.45119259881935 --lng -80.00498215509094
./target/debug/doublezero exchange create --code xam --name "Amsterdam" --lat 52.30085793004002 --lng 4.942241140085309

### Contributors
./target/doublezero contributor create --code co01

### Devices
./target/debug/doublezero device create --code la2-dz01 --contributor co01 --location la --exchange xla --public-ip "207.45.216.136" --dz-prefix "207.45.216.136/29"
./target/debug/doublezero device create --code ny5-dz01 --contributor co01 --location ny --exchange xny --public-ip "64.86.249.80" --dz-prefix "64.86.249.80/29"
./target/debug/doublezero device create --code ld4-dz01 --contributor co01 --location ld --exchange xld --public-ip "195.219.120.72" --dz-prefix "195.219.120.72/29"
./target/debug/doublezero device create --code frk-dz01 --contributor co01 --location fr --exchange xfr --public-ip "195.219.220.88" --dz-prefix "195.219.220.88/29"
./target/debug/doublezero device create --code sg1-dz01 --contributor co01 --location sg --exchange xsg --public-ip "180.87.102.104" --dz-prefix "180.87.102.104/29"
./target/debug/doublezero device create --code ty2-dz01 --contributor co01 --location ty --exchange xty --public-ip "180.87.154.112" --dz-prefix "180.87.154.112/29"
./target/debug/doublezero device create --code pit-dzd01 --contributor co01 --location pi --exchange xpi --public-ip "204.16.241.243" --dz-prefix "204.16.243.243/32"
./target/debug/doublezero device create --code ams-dz001 --contributor co01 --location ams --exchange xam --public-ip "195.219.138.50" --dz-prefix "195.219.138.56/29"


### Links
./target/debug/doublezero link create wan --code "la2-dz01:ny5-dz01" --contributor co01 --side-a la2-dz01 --side-z ny5-dz01 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 3
./target/debug/doublezero link create wan --code "ny5-dz01:ld4-dz01" --contributor co01 --side-a ny5-dz01 --side-z ld4-dz01 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 3
./target/debug/doublezero link create wan --code "ld4-dz01:frk-dz01" --contributor co01 --side-a ld4-dz01 --side-z frk-dz01 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 25 --jitter-ms 10
./target/debug/doublezero link create wan --code "ld4-dz01:sg1-dz01" --contributor co01 --side-a ld4-dz01 --side-z sg1-dz01 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 120 --jitter-ms 9
./target/debug/doublezero link create wan --code "sg1-dz01:ty2-dz01" --contributor co01 --side-a sg1-dz01 --side-z ty2-dz01 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 7
./target/debug/doublezero link create wan --code "ty2-dz01:la2-dz01" --contributor co01 --side-a ty2-dz01 --side-z la2-dz01 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 10


# pit
#./target/debug/doublezero user create --device 6HYr3JYsVvdGARWAYyvVAVoRZFKoLpufmVebGHD4xYAm --client-ip 145.40.78.112


#./target/debug/doublezero user activate --client-ip 145.40.78.113 --Link-net 192.168.2.0/31
