coredns-tailscale
=================

A CoreDNS plugin implementation for Tailscale networks that integrates with Tailscale via socket communication.

<img width="935" height="934" alt="example-com" src="https://github.com/user-attachments/assets/0b4d0388-ebb4-499b-8558-138c54742c8e" />

Rationale
---------

While Tailscale's MagicDNS provides excellent built-in DNS functionality, there are several scenarios where using your own domain with CoreDNS offers significant advantages:

### Limitations of MagicDNS

**Domain Control**: MagicDNS uses the `ts.net` domain (e.g., `myserver.tailnet-name.ts.net`), which you don't control. You cannot:
- Use your own branded domain names
- Integrate with existing internal DNS infrastructure
- Create custom subdomains for different services
- Use SSL certificates for your own domain

**Service Discovery**: MagicDNS maps one hostname per Tailscale node. This doesn't work well when:
- Multiple services run on the same node (web server, API, database)
- You need logical service names that don't match machine names
- You want round-robin DNS for load balancing across multiple nodes
- You need CNAME records for service aliases

**Enterprise Integration**: Many organizations require:
- DNS records in their own domain namespace
- Integration with existing DNS management workflows
- Consistent naming conventions across internal and external services
- Support for complex DNS record types and configurations

### Advantages of This Plugin

This CoreDNS plugin bridges the gap by:
- **Custom Domains**: Use `api.company.com` instead of `api-server.tailnet.ts.net`
- **Service Mapping**: Map multiple service names to the same Tailscale node
- **Load Balancing**: Automatic round-robin DNS for numbered hostnames (`web-1`, `web-2` → `web`)
- **CNAME Support**: Create service aliases via Tailscale node tags
- **Enterprise DNS**: Integrate Tailscale nodes into existing DNS infrastructure

Features
--------
This plugin for CoreDNS allows the following:

1. Automatically serving an (arbitrary) DNS zone with each Tailscale server in your Tailnet added with A and AAAA records.
2. Allowing CNAME records to be defined via Tailscale node tags that link logical names to Tailscale machines.
3. Socket-based communication with Tailscale daemon for real-time node information.

Architecture
-----------

The plugin uses a sidecar architecture with two containers:
- **CoreDNS container**: Runs the custom CoreDNS build with the Tailscale plugin
- **Tailscale container**: Runs Tailscale daemon and exposes socket for communication

The containers communicate via a shared Unix socket (`/tmp/tailscale/tailscaled.sock`).

Configuration
-------------

### CoreDNS Plugin Configuration (Corefile)

Example configuration (see `example/Corefile`):

```
nodes.example.com {
    bind 10.53.53.53
    tailscale nodes.example.com {
        socket /tmp/tailscale/tailscaled.sock
        fallthrough
    }
    log
    errors
}
```

The `socket` directive specifies the path to the Tailscale daemon socket. If not specified, defaults to `/tmp/tailscale/tailscaled.sock`.

### Docker Compose Configuration

Example configuration (see `example/compose.coredns-tailscale.yml`):

```yaml
name: coredns
services:
  coredns:
    build:
      context: ../
    volumes:
      - ./configurations/coredns-tailscale:/etc/coredns
      - tailscale-socket:/tmp/tailscale
    networks:
      coredns_net:
        ipv4_address: 10.53.53.53
    restart: unless-stopped
    depends_on:
      - tailscale

  tailscale:
    image: tailscale/tailscale:latest
    hostname: coredns
    environment:
      - TS_AUTHKEY=${TS_AUTH_KEY__COREDNS}
      - TS_STATE_DIR=/var/lib/tailscale
      - TS_ROUTES=10.53.53.0/24
      - TS_ACCEPT_DNS=false
      - TS_SOCKET=/tmp/tailscale/tailscaled.sock
    volumes:
      - tailscale-state:/var/lib/tailscale
      - tailscale-socket:/tmp/tailscale
    cap_add:
      - NET_ADMIN
      - NET_RAW
    sysctls:
      - net.ipv4.ip_forward=1
      - net.ipv6.conf.all.forwarding=1
    networks:
      coredns_net:
        ipv4_address: 10.53.53.2
    restart: unless-stopped

networks:
  coredns_net:
    driver: bridge
    ipam:
      config:
        - subnet: 10.53.53.0/24

volumes:
  tailscale-state:
  tailscale-socket:
```

### Environment Variables

#### Creating a Tailscale Auth Key

1. Navigate to [Tailscale Admin Console > Settings > Keys](https://login.tailscale.com/admin/settings/keys)
2. Click "Generate auth key"
3. Configure the key settings:
   - **Description**: "CoreDNS Server" (or similar descriptive name)
   - **Reusable**: Enable if you plan to recreate containers
   - **Ephemeral**: Disable (recommended for persistent DNS servers)
   - **Preauthorized**: Enable to avoid manual device approval
   - **Tags**: Add `tag:dns` if you configured ACL auto-approval (see below)
4. Click "Generate key"
5. Copy the generated key (format: `tskey-auth-...`)

#### Setting the Auth Key

Set the Tailscale auth key as an environment variable:

```bash
export TS_AUTH_KEY__COREDNS=tskey-auth-YOUR_GENERATED_KEY_HERE
```

Or create a `.env` file:
```
TS_AUTH_KEY__COREDNS=tskey-auth-YOUR_GENERATED_KEY_HERE
```

**Security Note**: Keep your auth keys secure and never commit them to version control. Consider using environment variable management tools in production.

Usage
-----

1. Build and start the services:
   ```bash
   docker-compose -f compose.coredns-tailscale.yml up -d
   ```

2. Test DNS resolution:
   ```bash
   dig coredns.nodes.example.com @10.53.53.53
   dig gitea.nodes.example.com @10.53.53.53
   ```

3. Configure Tailscale to use the DNS server (see Tailscale Configuration below).

Multiple Records and Round-Robin DNS
----------------------------------

The plugin automatically creates multiple A/AAAA records for load balancing scenarios:

### Hostname Grouping for Round-Robin DNS

When Tailscale nodes have similar hostnames with numerical suffixes (e.g., `web-1`, `web-2`, `web-3`), the plugin automatically creates grouped records for round-robin DNS:

**Individual node records:**
```
web-1.nodes.example.com.  IN A 100.64.0.1
web-2.nodes.example.com.  IN A 100.64.0.2
web-3.nodes.example.com.  IN A 100.64.0.3
```

**Grouped record (automatically created):**
```
web.nodes.example.com.    IN A 100.64.0.1
web.nodes.example.com.    IN A 100.64.0.2
web.nodes.example.com.    IN A 100.64.0.3
```

This allows clients to query `web.nodes.example.com` and receive all IP addresses for load balancing, while still being able to target specific instances with `web-1.nodes.example.com`, etc.

### IPv4 and IPv6 Support

Each node automatically gets both A and AAAA records when available:
```
test-machine.nodes.example.com. IN A    100.64.0.10
test-machine.nodes.example.com. IN AAAA fd7a:115c:a1e0::1
```

CNAME records via Tags
---------------------

A CNAME record can be added to point to a machine by simply creating a Tailscale machine tag prefixed by `cname-`. Any text in the tag after that prefix will be used to generate the resulting CNAME entry, so for example, the tag `cname-friendly-name` on a machine named `test-machine` will result in the following DNS records:

```
friendly-name IN CNAME test-machine.nodes.example.com.
test-machine  IN A <Tailscale IPv4 Address>
test-machine  IN AAAA <Tailscale IPv6 Address>
```

Tailscale Integration
-------------------

The plugin connects to the Tailscale daemon via Unix socket and automatically updates DNS entries when:
- New nodes join the Tailnet
- Existing nodes change IP addresses
- Nodes leave the Tailnet

Only machines reachable from the hosting Tailscale machine will be configured in DNS (those shown in `tailscale status` output). This approach avoids the need for managing expiring Tailscale API tokens.

Tailscale Configuration
---------------------

### ACL Configuration (Optional but Recommended)

To automatically approve subnet routes for your DNS server, add the following to your [Tailscale ACL file](https://login.tailscale.com/admin/acls/file):

```json
{
    // Define the tags which can be applied to devices and by which users.
    "tagOwners": {
        "tag:dns":       ["autogroup:admin"],
        "tag:container": ["autogroup:admin"],
    },

    // Auto-approve static route for DNS servers
    "autoApprovers": {
        "routes": {
            "10.53.53.0/24": ["tag:dns"],
        },
    },
}
```

Then tag your CoreDNS Tailscale node with `tag:dns` in the [Tailscale admin console](https://login.tailscale.com/admin/machines).

### DNS Server Configuration

To configure Tailscale to use your CoreDNS server for specific domains:

1. **Enable subnet routing** in your Tailnet to make the DNS server accessible:
   - The Tailscale container automatically advertises the route `10.53.53.0/24`
   - If you configured ACLs with auto-approval (see above), tag the node with `tag:dns`
   - Otherwise, manually approve the subnet route in the [Tailscale admin console](https://login.tailscale.com/admin/machines)

2. **Configure nameservers** in the [Tailscale DNS settings](https://login.tailscale.com/admin/dns):
   - Navigate to the "Nameservers" section
   - Click "Add nameserver"
   - Enter 10.53.53.53 as the nameserver. Don't enable `Restrict to domain` for your domain (e.g. `example.com`).

3. **Verify the configuration**:
   - The nameserver entries should show `example.com` `Split DNS`.
   - Test from any Tailscale client:
     ```bash
     dig coredns.nodes.example.com
     dig gitea.nodes.example.com
     ```

**Note**: The DNS server will only be accessible to devices on your Tailnet after the subnet route is approved and the nameserver configuration is applied.

Building
--------

The plugin is built as part of a custom CoreDNS image using a multi-stage Dockerfile that:
1. Clones the CoreDNS source
2. Integrates the Tailscale plugin
3. Builds a minimal Alpine-based image with the custom CoreDNS binary
