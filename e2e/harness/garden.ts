// The seeded people and plants the tests refer to, copied from the Go seed.
// The ids are the ones the seed writes, so a test can find a row by id without
// querying the database.

export const people = {
  ellie: { name: 'Ellie', handle: 'ellie' },
  sam: { name: 'Sam', handle: 'sam' },
} as const;

// The devices Ellie has registered a passkey on, one row each on Passkeys.
export const devices = {
  phone: 'iPhone',
  laptop: 'MacBook Air',
} as const;

// The browsers Ellie has subscribed to notifications in, one row each under
// "Where they arrive". The server builds each name from the User-Agent the
// seed writes. A User-Agent names no model, so the laptop's row says Mac.
export const browsers = {
  phone: 'iPhone · Safari',
  laptop: 'Mac · Chrome',
} as const;

// The recovery codes the seed gives Ellie: ten made, two of them used.
export const recoveryBatch = { left: 8, size: 10 } as const;

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
  // The seed gives Sprout no location, so Plants lists it under No room.
  sprout: { id: '00000000-0000-7000-8000-050000000012', name: 'Sprout' },
} as const;

// No room is the heading Plants uses for plants with no location. It is not a
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
