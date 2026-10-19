// Which addresses are on the person's own network (private_net.js): what a
// vault card may not fetch from a public page.
// `CHROME_PATH=/nonexistent node --test chrome-extension/private_net.test.mjs`

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

const { MonoPrivateNet: P } = loadExtensionScripts(["private_net.js"]);

test("private network addresses are told from public ones, in every form an address takes", () => {
  const host = (u) => new URL(u).hostname;
  for (const u of ["http://127.0.0.1/", "http://127.9.9.9/", "http://10.1.2.3/", "http://172.16.0.1/", "http://172.31.255.255/", "http://192.168.1.1/",
    "http://169.254.169.254/", "http://0.0.0.0/", "http://[::1]/", "http://[fc00::1]/", "http://[fd12:3456::1]/", "http://[fe80::1]/",
    "http://[::ffff:127.0.0.1]/", "http://2130706433/", "http://0x7f.1/", "http://localhost/", "http://app.localhost/", "http://printer.local/",
    "http://intranet/", "http://db.internal/"]) assert.equal(P.isPrivateHost(host(u)), true, u);
  for (const u of ["https://a.example/", "http://172.32.0.1/", "http://8.8.8.8/", "http://[2001:db8::1]/", "http://192.169.1.1/"]) {
    assert.equal(P.isPrivateHost(host(u)), false, u);
  }
});

// Vault card review: the rest of the special-use ranges, the IPv6 forms that
// carry an IPv4 address, the home and office network names, and the known
// wildcard-DNS services (a public name that resolves to whatever it spells).
test("special-use ranges, embedded IPv4, network names and wildcard-DNS names are private", () => {
  const host = (u) => new URL(u).hostname;
  for (const u of ["http://100.64.0.1/", "http://100.100.100.200/", "http://100.127.255.255/", "http://198.18.0.1/", "http://198.19.255.255/",
    "http://192.0.0.8/", "http://224.0.0.1/", "http://239.255.255.250/", "http://255.255.255.255/", "http://240.0.0.1/",
    "http://[::127.0.0.1]/", "http://[64:ff9b::7f00:1]/", "http://[64:ff9b::a00:1]/", "http://[2002:7f00:1::]/", "http://[2002:c0a8:101::1]/",
    "http://[fec0::1]/", "http://[ff02::1]/",
    "http://nas.lan/", "http://router.home/", "http://printer.home.arpa/", "http://wiki.corp/", "http://db.localdomain/", "http://hr.intranet/",
    "http://vault.private/", "http://laptop.tail1234.ts.net/",
    "http://127.0.0.1.nip.io/", "http://app.10.0.0.1.sslip.io/", "http://x.xip.io/", "http://localtest.me/", "http://a.localtest.me/",
    "http://lvh.me/", "http://app.lvh.me/", "http://x.traefik.me/"]) assert.equal(P.isPrivateHost(host(u)), true, u);
  for (const u of ["http://100.63.255.255/", "http://100.128.0.1/", "http://198.17.0.1/", "http://198.20.0.1/", "http://192.0.1.1/",
    "http://223.255.255.255/", "http://[2001:4860::1]/", "http://[64:ff9b::808:808]/", "http://[2002:808:808::]/",
    "http://home.example.com/", "http://nip.io.example.com/", "http://corp.example/", "http://lvh.me.example.org/"]) {
    assert.equal(P.isPrivateHost(host(u)), false, u);
  }
});

// Vault card review: a page is on the person's network only by a private IP
// literal, localhost or *.localhost. A wildcard-DNS name, a tailnet name or an
// office suffix can be served from anywhere (203-0-113-5.sslip.io resolves to
// a public server, *.ts.net can be Tailscale Funnel), so such a page is public.
test("a page is private only by a private IP literal, localhost or *.localhost", () => {
  const host = (u) => new URL(u).hostname;
  for (const u of ["http://127.0.0.1:8080/", "http://localhost/", "http://app.localhost/", "http://10.0.0.5/", "http://192.168.1.10/", "http://[::1]/", "http://[fd12::1]/"]) {
    assert.equal(P.isPrivatePage(host(u)), true, u);
  }
  for (const u of ["https://203-0-113-5.sslip.io/", "https://x.ts.net/", "https://nip.io/", "http://127.0.0.1.nip.io/", "https://router.lan/",
    "http://printer.local/", "http://intranet/", "http://db.internal/", "http://lvh.me/", "https://a.example/"]) {
    assert.equal(P.isPrivatePage(host(u)), false, u);
    assert.equal(P.isPrivatePage(""), false, "no host is no private page");
  }
});

// Vault card review: more wildcard-DNS services, and the IPv6 forms that carry
// an IPv4 address in other ways.
test("more wildcard-DNS names, IPv4-translated IPv6 and Teredo are private items", () => {
  const host = (u) => new URL(u).hostname;
  for (const u of ["http://x.1u.ms/", "http://127.0.0.1.xip.name/", "http://a.vcap.me/", "http://lacolhost.com/", "http://x.fbi.com/",
    "http://localho.st/", "http://a.local.gd/", "http://[::ffff:0:127.0.0.1]/", "http://[::ffff:0:a00:1]/", "http://[2001:0:4136:e378:8000:63bf:3fff:fdd2]/"]) {
    assert.equal(P.isPrivateHost(host(u)), true, u);
  }
  assert.equal(P.isPrivateHost(host("http://[::ffff:0:808:808]/")), false, "a public IPv4 inside stays public");
  assert.equal(P.isPrivateHost(host("http://[2001:db8::1]/")), false);
});
