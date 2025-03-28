# DoubleZero Controller/Agent

The DoubleZero controller and agent are responsible for the management of DoubleZero devices, including configuration and telemetry collection. 

## Build/Install Instructions

Install Go as a dependency: https://go.dev/doc/install

To build the controller:
```
$ cd controller
$ make build

$ ./bin/controller start -h
Usage of start:
  -listen-addr string
        listening address for controller grpc server (default "localhost")
  -listen-port string
        listening port for controller grpc server (default "443")
  -program-id string
        smartcontract program id to monitor
```

To build the agent:
```
$ cd agent
$ make build

$ ./bin/doublezero-agent -h
Usage of ./bin/doublezero-agent:
  -controller string
        The DoubleZero controller IP address and port to connect to (default "18.116.166.35:7000")
  -controller-timeout-in-seconds float
        How long to wait for a response from the controller before giving up (default 2)
  -device string
        IP Address and port of the Arist EOS API. Should always be the local switch at 127.0.0.1:9543. (default "127.0.0.1:9543")
  -max-lock-age-in-seconds int
        If agent detects a config lock that older than the specified age, it will force unlock. (default 3600)
  -pubkey string
        This device's public key on the doublezero network (default "frtyt4WKYudUpqTsvJzwN6Bd4btYxrkaYNhBNAaUVGWn")
  -sleep-interval-in-seconds float
        How long to sleep in between polls (default 5)
  -verbose
        Enable verbose logging
  -version
        version info
```

To install the agent on Arista EOS:

## Prerequisites
1. Supported network hardware: Arista Networks 7130 and 7280 switchs
1. Admin access to the Arista switch(es) that you’ll be joining to the DoubleZero network
1. Each device’s doublezero public key, generate by running the command `doublezero device create`
1. Determine which Arista routing instance the agent will use to connect to the DoubleZero Controller. If you can ping the controller with `ping <W.X.Y.Z>` where W.X.Y.Z is the IP address of the DoubleZero controller, you will use the default routing instance, named `default`. If you need to specify a vrf, for example with `ping vrf management <W.X.Y.Z>`, then (in this example) your routing instance would be `management`
## Agent Installation Steps

Use these steps if your DoubleZero Agent will connect to the DoubleZero Controller using Arista's default routing instance. 

1. To allow agents running on the local device, including doublezero-agent, to call the local device’s API, enter the following into the EOS configuration:

    ```
    !
    ! Replace the word "default" with the VRF name you identified in prerequisites step 4
    !
    management api eos-sdk-rpc
       transport grpc eapilocal
          localhost loopback vrf default
          service all
          no disabled
    ```

2. Download and install the current stable doublezero-agent binary package
    1. As admin on the EOS CLI, run the "bash" command and then enter following commands:

    ```
    switch# bash
    $ sudo bash
    # cd /mnt/flash
    # wget https://dl.cloudsmith.io/public/malbeclabs/doublezero/rpm/any-distro/any-version/x86_64/doublezero-agent_0.0.7_linux_amd64.rpm
    # exit
    $ exit
    ```
    !!! note
        You can find more info about Arista EOS extensions [here](https://www.arista.com/en/um-eos/eos-managing-eos-extensions)
    2. Back on the EOS CLI, set up the agent
    ```
    switch# copy flash:doublezero-agent_0.0.7_linux_amd64.rpm extension:
    switch# extension doublezero-agent_0.0.7_linux_amd64.rpm
    switch# copy installed-extensions boot-extensions
    ```
    3. Verify the extension

    The Status should be "A, I, B".
    ```
    switch# show extensions
    Name                                           Version/Release      Status       Extension
    ---------------------------------------------- -------------------- ------------ ---------
    doublezero-agent_0.0.7_linux_amd64.rpm      0.0.7/1              A, I, B      1

    A: available | NA: not available | I: installed | F: forced | B: install at boot
    ```

3. To set up and start the agent, go back to EOS command line, add the following to the Arista EOS configuration:
    1. Configure the doublezero agent
    ```
    !
    ! Replace the word `default` in the exec command below with the VRF name you identified in prerequisites step 4
    !
    daemon doublezero-agent
    exec ip netns exec ns-default /usr/local/bin/doublezero-agent -pubkey <PUBKEY>
    no shut
    ```
    2. Verify that the agent is working
    When the agent is up and running you should see the following log entries:

    ```
    switch# ceos2#show agent doublezero-agent logs
    2025/01/21 18:17:52 main.go:71: Starting doublezero-agent
    2025/01/21 18:17:52 main.go:72: doublezero-agent controller: 18.116.166.35:7000
    2025/01/21 18:17:52 main.go:73: doublezero-agent sleep-interval-in-seconds: 5.000000
    2025/01/21 18:17:52 main.go:74: doublezero-agent controller-timeout-in-seconds: 2.000000
    2025/01/21 18:17:52 main.go:75: doublezero-agent pubkey: 111111G5zfGFHe9aek69vLPkXTZnkozyBm468PhitD7U
    2025/01/21 18:17:52 main.go:76: doublezero-agent device: 127.0.0.1:9543
    2025/01/21 18:17:52 dzclient.go:32: controllerAddressAndPort 18.116.166.35:7000
    ```

## Agent Upgrade Steps

1. Download the latest version of agent:

```
switch# bash
$ sudo bash
# cd /mnt/flash
# wget https://dl.cloudsmith.io/public/malbeclabs/doublezero/rpm/any-distro/any-version/x86_64/doublezero-agent_0.0.7_linux_amd64.rpm
# exit
$ exit
```
2. Back on the EOS CLI, remove the old version
First, find the filename of the old version. It should look like `doublezero-agent_X.Y.Z_linux_amd64.rpm`
```
switch# show extensions
```
Run the following commands to remove the old version. Replace the filenames below with the one from the `show extensions` command above.
```
switch# delete flash:doublezero-agent_X.Y.Z_linux_amd64.rpm
switch# delete extension:doublezero-agent_X.Y.Z_linux_amd64.rpm
```
3. Set up the new agent version
```
switch# copy flash:doublezero-agent_0.0.7_linux_amd64.rpm extension:
switch# extension doublezero-agent_0.0.7_linux_amd64.rpm
switch# copy installed-extensions boot-extensions
```
4. Verify the extension

The Status should be "A, I, B".
```
switch# show extensions
Name                                           Version/Release      Status       Extension
---------------------------------------------- -------------------- ------------ ---------
doublezero-agent_0.0.7_linux_amd64.rpm      0.0.7/1              A, I, B      1

A: available | NA: not available | I: installed | F: forced | B: install at boot
```
## FAQ

Q: How can I see the agent’s logs or output?

A: Run the following EOS command:
```
show agent doublezero-agent log
