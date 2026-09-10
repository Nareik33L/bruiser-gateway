# Stretch — Cloudflare Worker Edge

This directory is the **stretch** reference Edge for Harchester: a Worker that
calls Bruiser `POST /v1/authorize` and forwards allowed allocation requests to
SimTix origin, same contract as `demos/simtix` Go Edge.

It is **not** a rewrite of the demonstration environment. The demo remains the
Compose stack (club, SimTix origin, admin, load-lab, Postgres, real gateway) on
a VM, with Cloudflare DNS + proxy in front. See
[docs/09-harchester-cloudflare.md](../../../docs/09-harchester-cloudflare.md).

Do not deploy a `workers.dev` stand-in of Bruiser, club, or SimTix from here.
