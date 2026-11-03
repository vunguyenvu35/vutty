package updater

// PublicKeyBase64 is the key that releases are signed with: an update that was not signed by its private half is refused. The
// private key stays with the maintainer (see tools/updatesign and docs/ReleaseSigning.md). Changing this key means that every
// installed copy has to be updated by hand once, since it can only trust what it already carries.
const PublicKeyBase64 = "cxVrgtEMwGXay1d51T2uSFpn4ilhnvxORDTjOj3LvKc="
