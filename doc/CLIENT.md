# CGC 
> cgc is a client management CLI tool for chain gateway.


## Installation

```bash
sudo make install
```

Prepare a .env file in the current directory with the following content:
```env
CGC_USER=
CGC_PASSWORD=
```

## Usage

### Retrieve Resources

```bash
cgc get <resource>
```
Resource can be upstream or rule.

Example: Retrieve ready upstreams for Ethereum mainnet.

```bash
cgc get upstream -r --chainId=1
```

## Import Resources

```bash
cgc import <resource>
```
Resource can be upstream or rule.

Example: Import rules to override existing rules.

```bash
cgc import rule --file ./check_rule/{your rule}.json
```

## Generate AKSK

```bash
cgc gen secret --group <group> --service <service>
```

## Check Upstream

```bash
cgc check upstream --chainIds 1,80002
```
Use this command when you want newly imported rules to take effect immediately for a group of chainIds' upstreams.






