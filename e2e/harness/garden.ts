// The seeded people and plants the tests refer to, copied from the Go seed.
// The ids are the ones the seed writes, so a test can find a row by id without
// querying the database.

// Jo and Clare are sitters. Jo's access ends in eight days and Clare's ended
// twelve days ago, the two states a membership with no end date has not got.
// Clare is in no other garden, so signing in as Clare lands on the "You're in
// no garden" page.
export const people = {
  ellie: { name: 'Ellie', handle: 'ellie' },
  sam: { name: 'Sam', handle: 'sam' },
  jo: { name: 'Jo', handle: 'jo' },
  clare: { name: 'Clare', handle: 'clare' },
  robin: { name: 'Robin', handle: 'robin' },
} as const;

// The devices Ellie has registered a passkey on, one row each on Passkeys.
export const devices = {
  phone: 'iPhone',
  laptop: 'MacBook Air',
} as const;

// The browsers Ellie has subscribed to notifications in, one row each under
// Subscribed devices. The server builds each name from the User-Agent the
// seed writes. A User-Agent names no model, so the laptop's row says Mac.
export const browsers = {
  phone: 'iPhone · Safari',
  laptop: 'Mac · Chrome',
} as const;

// The two gardens in the seed. A garden's name is the heading on Today while
// the session is on it. Ellie owns Home and is in no other garden. Robin owns
// Upstairs. Sam is a member of Home and a sitter in Upstairs, so the switching
// tests sign in as Sam.
export const gardens = {
  home: { name: 'Home' },
  upstairs: { name: 'Upstairs' },
} as const;

// The one pending invite on People: a sitter's, made two days ago. Its token
// is the plaintext the seed hashes, so a test can open the link.
export const invites = {
  sitter: { token: 'development-sitter-invite' },
} as const;

// The recovery codes the seed gives Ellie: ten made, two of them used. unused
// and used are the plaintext of two of the ten, so a test can post a code the
// seed hashed.
export const recoveryBatch = {
  left: 8,
  size: 10,
  unused: 'b9xa-3fkt-7rjw',
  used: 'k4rt-9wme-3xqd',
} as const;

// The care types the garden records against, in the order the Garden page
// lists them. Mist was tried and turned off, and its row is still there.
export const careTypes = {
  water: 'Water',
  feed: 'Feed',
  repot: 'Repot',
  mist: 'Mist',
} as const;

export const plants = {
  bigFella: { id: '00000000-0000-7000-8000-050000000001', name: 'Big Fella' },
  gerald: { id: '00000000-0000-7000-8000-050000000002', name: 'Gerald' },
  doris: { id: '00000000-0000-7000-8000-050000000003', name: 'Doris' },
  nigel: { id: '00000000-0000-7000-8000-050000000005', name: 'Nigel' },
  trailMix: { id: '00000000-0000-7000-8000-050000000007', name: 'Trail Mix' },
  motherInLaw: { id: '00000000-0000-7000-8000-050000000004', name: 'Mother-in-Law' },
  // The seed gives this plant no nickname, so the page shows its common name.
  goldenPothos: { id: '00000000-0000-7000-8000-050000000008', name: 'Golden pothos' },
  littleFella: { id: '00000000-0000-7000-8000-050000000009', name: 'Little Fella' },
  // Opuntia microdasys has only a botanical name.
  opuntia: { id: '00000000-0000-7000-8000-050000000011', name: 'Opuntia microdasys' },
  // The seed gives Sprout no room, so Plants lists it under No room.
  sprout: { id: '00000000-0000-7000-8000-050000000012', name: 'Sprout' },
} as const;

// No room is the heading Plants uses for plants with no room set. It is not a
// room the seed writes.
export const rooms = {
  bathroom: 'Bathroom',
  bedroom: 'Bedroom',
  kitchen: 'Kitchen',
  livingRoom: 'Living room',
  windowsill: 'Windowsill',
  noRoom: 'No room',
} as const;

export type Plant = (typeof plants)[keyof typeof plants];

// The tokens in the Home garden. The spare expired six days ago, so the list
// has one live row and one expired one.
export const tokens = {
  kitchen: 'The kitchen display',
  spare: 'The spare display',
} as const;
