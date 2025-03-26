# DoubleZero Client Build/Install

The DoubleZero client can be either installed as a apt/deb or rpm package or built from source. We _highly_ suggest using the stable version noted in the "How to connect to DoubleZero" section of the documentation here: https://docs.malbeclabs.com/connect/

## Package Installation

### Add Debian/APT Repository

#### Scripted Install
```
$ curl -1sLf \
  'https://dl.cloudsmith.io/public/malbeclabs/doublezero/setup.deb.sh' \
  | sudo -E bash

$ apt install doublezero-x.x.x
```

#### Manual Install
```
$ apt-get install -y debian-keyring  # debian only
$ apt-get install -y debian-archive-keyring  # debian only
$ apt-get install -y apt-transport-https

# For Debian Stretch, Ubuntu 16.04 and later
$ keyring_location=/usr/share/keyrings/malbeclabs-doublezero-archive-keyring.gpg

# For Debian Jessie, Ubuntu 15.10 and earlier
$ keyring_location=/etc/apt/trusted.gpg.d/malbeclabs-doublezero.gpg

$ curl -1sLf 'https://dl.cloudsmith.io/public/malbeclabs/doublezero/gpg.EC4BD0DD63EC1762.key' |  gpg --dearmor >> ${keyring_location}
$ curl -1sLf 'https://dl.cloudsmith.io/public/malbeclabs/doublezero/config.deb.txt?distro=ubuntu&codename=xenial&component=main' > /etc/apt/sources.list.d/malbeclabs-doublezero.list

$ sudo chmod 644 ${keyring_location}
$ sudo chmod 644 /etc/apt/sources.list.d/malbeclabs-doublezero.list

$ apt-get update

$ apt install doublezero=x.x.x
```

### Yum Repository

#### Scripted Install
```
$ curl -1sLf \
  'https://dl.cloudsmith.io/public/malbeclabs/doublezero/setup.rpm.sh' \
  | sudo -E bash

$ yum install doublezero-x.x.x
```

#### Manual Install
```
$ yum install yum-utils pygpgme

$ rpm --import 'https://dl.cloudsmith.io/public/malbeclabs/doublezero/gpg.EC4BD0DD63EC1762.key'

$ curl -1sLf 'https://dl.cloudsmith.io/public/malbeclabs/doublezero/config.rpm.txt?distro=el&codename=7' > /tmp/malbeclabs-doublezero.repo

$ yum-config-manager --add-repo '/tmp/malbeclabs-doublezero.repo'

$ yum -q makecache -y --disablerepo='*' --enablerepo='malbeclabs-doublezero'

$ yum install doublezero-x.x.x
```


## Building From Source

### Install Dependencies

Rust:
```
$ curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
```

Go:
https://go.dev/doc/install

### Build DoubleZero CLI/Daemon
```
# Checkout the latest stable version. You can find the latest stable version here: https://docs.malbeclabs.com/connect/
$ git checkout client/vX.X.X

# build the doublezero cli
$ cargo build --release --target-dir ./bin --manifest-path doublezero/Cargo.toml
$ mv bin/release/doublezero bin/.

# build the doublezero daemon
$ CGO_ENABLED=0 go build -o bin/doublezerod doublezerod/cmd/doublezerod/main.go

# The doublezero cli and the daemon communicate over a unix socket. Setup socket file directory and permissions
$ sudo mkdir /var/run/doublezerod
$ sudo chmod 700 /var/run/doublezerod
$ sudo chown $USER:$USER /var/run/doublezerod

# Setup start directory where the DoubleZero daemon stores connection state:
$ sudo mkdir /var/lib/doublezerod
$ sudo chmod 700 /var/lib/doublezerod
$ sudo chown $USER:$USER /var/lib/doublezerod
```

The DoubleZero daemon requires CAP_NET_ADMIN and CAP_NET_RAW capabilities. CAP_NET_ADMIN capability is for the ability to create tunnel interfaces, add IP addressing and add routes to the kernel routing table via netlink. CAP_NET_RAW capability is used for latency probing via raw sockets:
```
$ sudo setcap cap_net_raw,cap_net_admin=+ep ./bin/doublezerod

$ getcap bin/doublezerod
doublezerod cap_net_admin,cap_net_raw=ep
```

### Starting DoubleZero Client
```
$ ./bin/doublezerod &

$ ./bin/doublezero latency
 pubkey                                       | name      | ip             | min      | max      | avg      | reachable
 CCTSmqMkxJh3Zpa9gQ8rCzhY7GiTqK7KnSLBYrRriuan | ny5-dz01  | 64.86.249.22   |  20.71ms |  31.71ms |  24.85ms | true
 96AfeBT6UqUmREmPeFZxw6PbLrbfET51NxBFCCsVAnek | la2-dz01  | 207.45.216.134 |  79.72ms |  80.96ms |  80.34ms | true
 55tfaZ1kRGxugv7MAuinXP4rHATcGTbNyEKrNsbuVLx2 | ld4-dz01  | 195.219.120.66 |  97.24ms |  97.86ms |  97.46ms | true
 3uGKPEjinn74vd9LHtC4VJvAMAZZgU9qX9rPxtc6pF2k | ams-dz001 | 195.219.138.50 | 108.88ms | 110.47ms | 109.69ms | true
 65DqsEiFucoFWPLHnwbVHY1mp3d7MNM2gNjDTgtYZtFQ | frk-dz01  | 195.219.220.58 | 105.80ms | 116.99ms | 110.02ms | true
 BX6DYCzJt3XKRc1Z3N8AMSSqctV6aDdJryFMGThNSxDn | ty2-dz01  | 180.87.154.78  | 184.18ms | 186.08ms | 185.20ms | true
 9uhh2D5c14WJjbwgM7BudztdoPZYCjbvqcTPgEKtTMZE | sg1-dz01  | 180.87.102.98  | 257.16ms | 259.07ms | 258.22ms | true
```

We highly recommend to run the daemon as a systemd service, which happens by default if you use either of the deb/apt or rpm packages. If you would like to handle this yourself, feel free to read through and/or use our systemd unit file [here](https://github.com/malbeclabs/doublezero/blob/main/client/doublezerod/cmd/doublezerod/doublezerod.service) and deb/apt/rpm packaging scripts [here](https://github.com/malbeclabs/doublezero/tree/main/client/packaging/scripts/doublezerod) for inspiration.

Once running, refer to https://docs.malbeclabs.com/connect/ for the latest documentation on how to connect to DoubleZero.
