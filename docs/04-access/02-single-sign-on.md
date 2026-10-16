---
title: "Set up single sign-on"
summary: "Let the household sign in through an OpenID Connect provider such as Authelia, Authentik, Keycloak or Pocket ID."
---

photon-server can sign people in through an OpenID Connect provider you already run. The server is
registered there as a confidential client using the authorization code flow, with PKCE, a state
bound to the browser and a nonce. An account is known by its provider and its subject, never by its
email, and signs in as the profile that linked it, or as one the server makes for it.

By the end of this page the login page offers **Continue with** your provider, and the people you
choose sign in with it.

## Before you start

The provider sends a browser back to the server's public address, so set **Public address** under
Settings › Server › Network first (see [Reach your server from outside](04-remote-access.md)).
Until it is set, Settings › Server › Sign-in offers no **Add a provider**.

People sign in with a provider from the web app at that public address: a sign-in started from
another address is refused with the address to use, because the provider sends the browser back to
the public one, and the cookie binding the sign-in goes only with a browser that left from there.

The provider's issuer must be reached over HTTPS, so its tokens and the client secret are never
sent in the clear; only an issuer on the server's own machine may use plain HTTP.

## Register the server at the provider

At your provider, make a client for photon-server:

- **Flow**: authorization code.
- **Redirect URI**: `https://photon.example/api/v1/auth/sign-in-providers/<slug>/callback`, where
  `<slug>` is the name you give the provider here. The form below shows it, ready to copy, as soon
  as you type the slug.
- **Scopes**: `openid`, `profile` and `email`; `groups` where you require a group; and
  `offline_access` where the server checks back every hour. The server asks for each only where
  the provider lists it.
- **Client authentication**: `client_secret_basic` or `client_secret_post`; the server uses the one
  the provider lists, preferring basic. A public client, with no secret, works too.

## Add the provider

Open Settings › Server › Sign-in and choose **Add a provider**. The server checks that the provider
answers at its issuer before it is added.

| Setting | What it does |
|---|---|
| Slug | Lower-case letters, digits and hyphens, up to 32. It names the provider in the redirect URI, so it cannot be changed later. |
| Redirect URI | Shown, not set: register it at the provider. |
| Name | What the login page calls it: "Continue with Pocket ID". |
| Issuer | The provider's address, exactly as its discovery document names it. Where the provider calls itself something else, the server says what, to use that instead. |
| Client id | The id of the client registered at the provider. |
| Client secret | Empty for a public client. Changing a provider, leave it empty to keep the secret, unless the issuer or client id changes. |
| Who signs in | **Only accounts linked to a profile** (the default), or **Anyone it signs in, each given a profile**. |
| Required group | Only accounts in this group at the provider, as its `groups` claim says, can sign in. Optional for linked accounts, required where the provider makes profiles. |
| Check back | **Every hour** (the default) or **Only as they sign in**. |

Where the provider makes profiles, three more settings say what those profiles see: **Every
library, including ones added later**, **Certificates** and **Titles with no certificate**, as for
any [profile](01-profiles.md#choose-what-a-profile-sees).

```sh
curl -X PUT https://photon.example/api/v1/admin/sign-in-providers/pocket-id \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"name": "Pocket ID", "issuer": "https://id.example.com", "client_id": "photon",
       "client_secret": "…", "provisioning": "link", "recheck": "hourly"}'
```

The card for each provider shows its redirect URI, whether a client secret is kept (never what it
is), how it checks back, its required group, and what the profiles it makes see.

## Who signs in

**Only accounts linked to a profile.** Each person links their account before they can sign in with
it: under Settings › Sign-in, **Link** their provider, sign in there, and come back linked. A
profile links one account at each provider, and an account links to one profile.

**Anyone it signs in, each given a profile.** Someone in the required group signing in for the
first time gets a profile of their own: a user, named by their username, their name or the start of
their email, with a number after it where a profile has that name already. It has no password; its
owner can set one under Settings › Profile, leaving the current password empty. A group is
required here, or anyone with an account at the provider would get a profile.

A refused sign-in comes back to the login page saying why: it took too long, the provider turned it
down, it could not be checked, the account is linked to no profile, it is not in the required group,
or it is linked to another profile already.

## Checking back

**Every hour**, the server asks the provider, by the refresh token it was given, whether each
account may still sign in. One the provider turns down, or that has left the required group, has
its devices signed out. A provider that does not answer signs no one out; it is asked again an
hour later. This needs the provider to offer offline access, and its refresh tokens to last longer
than the server might be down: Authelia's last 90 minutes unless you raise them.

**Only as they sign in** checks an account only when it signs in. Removing someone at the provider
then does not sign out their devices, so sign them out under
[Devices](07-devices-and-playback.md) too.

## Unlinking and removing

A profile unlinks its account under Settings › Sign-in. The devices it signed in with that account,
and any television paired from one of them, are signed out. A profile with no password and no other
linked account cannot unlink its only way in: it sets a password first.

Removing a provider from Settings › Server › Sign-in unlinks every account there and signs out the
devices they signed in. A profile with no password of its own can sign in again once an admin sets
it one under [Profiles](01-profiles.md#change-or-remove-a-profile). Moving a provider to another
issuer does the same, since the old subjects mean nothing to the new issuer.
