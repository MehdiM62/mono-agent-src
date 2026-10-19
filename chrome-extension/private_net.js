/**
 * MonoAgent Bridge — which addresses are on the person's own network
 *
 * A vault card (chat_proposals.js, vault_bridge.js) may save a file from the
 * page, and a public page must not have the extension fetch from the
 * person's own machine or network with it: a router, a NAS, a cloud metadata
 * service, a dev server. isPrivateHost reads a host as URL parsing writes it
 * (IPv4 in every form normalized to dotted decimal, IPv6 compressed in hex)
 * and says whether it is such an address, or a name that resolves to one.
 * It sees literal addresses and names only: a public name that resolves to a
 * private address (DNS) is not caught, beyond the known wildcard-DNS services
 * listed here.
 */

(function (root) {
  "use strict";

  const str = (v) => (typeof v === "string" ? v : "");

  // Names only a home, office or tailnet resolves, and the public wildcard-DNS
  // services that resolve a name to the address it spells (127.0.0.1.nip.io).
  const PRIVATE_NAMES =
    /(^|\.)(localhost|local|internal|lan|home|home\.arpa|corp|localdomain|intranet|private|ts\.net|nip\.io|sslip\.io|xip\.io|localtest\.me|lvh\.me|traefik\.me)$/;

  /** privateV4: this network, private, carrier-grade NAT, loopback, link-local, IETF and benchmarking ranges, multicast and reserved. */
  function privateV4(a, b, c) {
    return a === 0 || a === 10 || a === 127 || a >= 224 ||
      (a === 100 && b >= 64 && b <= 127) || (a === 169 && b === 254) || (a === 172 && b >= 16 && b <= 31) ||
      (a === 192 && b === 168) || (a === 192 && b === 0 && c === 0) || (a === 198 && (b === 18 || b === 19));
  }

  /** hextets are an IPv6 address's eight groups, as URL parsing writes it; null when it is not one. */
  function hextets(h) {
    const halves = h.split("::");
    if (halves.length > 2) return null;
    const groups = (part) => (part ? part.split(":") : []);
    const left = groups(halves[0]);
    const right = halves.length === 2 ? groups(halves[1]) : [];
    const all = [...left, ...Array(Math.max(0, 8 - left.length - right.length)).fill("0"), ...right];
    return all.length === 8 && all.every((x) => /^[0-9a-f]{1,4}$/.test(x)) ? all.map((x) => parseInt(x, 16)) : null;
  }

  /** privateV6: loopback, unspecified and an IPv4 address carried inside (::/96, ::ffff:0:0/96, NAT64, 6to4), ULA, link- and site-local, multicast. */
  function privateV6(g) {
    const v4 = (hi, lo) => privateV4(hi >> 8, hi & 255, lo >> 8);
    if (g.slice(0, 5).every((x) => x === 0) && (g[5] === 0 || g[5] === 0xffff)) return v4(g[6], g[7]);
    if (g[0] === 0x64 && g[1] === 0xff9b) return g[2] === 1 || v4(g[6], g[7]);
    if (g[0] === 0x2002) return v4(g[1], g[2]);
    return (g[0] & 0xfe00) === 0xfc00 || (g[0] & 0xffc0) === 0xfe80 || (g[0] & 0xffc0) === 0xfec0 || (g[0] & 0xff00) === 0xff00;
  }

  /**
   * isPrivateHost: a host (as URL parsing writes it) on the person's own
   * machine or network, or a name that resolves there: the private and
   * special-use IPv4 ranges (privateV4), the IPv6 ones and those carrying an
   * IPv4 address (privateV6), localhost and the home, office and tailnet
   * names, single-label names, and the known wildcard-DNS services. Literal
   * addresses and names only: any other public name that resolves privately
   * is not seen.
   */
  function isPrivateHost(host) {
    const h = str(host).toLowerCase().replace(/^\[|\]$/g, "").replace(/\.$/, "");
    if (!h) return true;
    if (PRIVATE_NAMES.test(h) || (!h.includes(".") && !h.includes(":"))) return true;
    const v4 = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.\d{1,3}$/.exec(h);
    if (v4) return privateV4(Number(v4[1]), Number(v4[2]), Number(v4[3]));
    if (!h.includes(":")) return false;
    const groups = hextets(h);
    return !groups || privateV6(groups); // an address that will not parse is not trusted
  }

  root.MonoPrivateNet = { isPrivateHost };
})(globalThis);
