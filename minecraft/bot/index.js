'use strict';

const mineflayer = require('mineflayer');
const { pathfinder, Movements, goals } = require('mineflayer-pathfinder');
const { GoalNear, GoalFollow, GoalGetToBlock, GoalXZ } = goals;

// ── Configuration ────────────────────────────────────────────────────────────
const HOST       = process.env.MC_HOST     || 'minecraft.minecraft.svc.cluster.local';
const PORT       = parseInt(process.env.MC_PORT || '25565', 10);
const USERNAME   = process.env.BOT_USERNAME || 'Bot';
const ROLE       = (process.env.BOT_ROLE   || 'wander').toLowerCase();
const MC_VERSION = process.env.MC_VERSION  || '1.21.11';

const RECONNECT_BASE_MS = 10_000;
const RECONNECT_MAX_MS  = 120_000;
let   reconnectDelay    = RECONNECT_BASE_MS;

console.log(`[${USERNAME}] Starting with role="${ROLE}" → ${HOST}:${PORT}`);

// ── Bot lifecycle ─────────────────────────────────────────────────────────────
function createBot() {
  const bot = mineflayer.createBot({
    host: HOST,
    port: PORT,
    username: USERNAME,
    version: MC_VERSION,
    auth: 'offline',
  });

  bot.loadPlugin(pathfinder);

  bot.once('spawn', () => {
    reconnectDelay = RECONNECT_BASE_MS;
    console.log(`[${USERNAME}] Spawned. Role: ${ROLE}`);

    const movements = new Movements(bot);
    movements.allowSprinting = true;
    movements.canDig = false; // don't grief player builds
    bot.pathfinder.setMovements(movements);

    bot.on('path_update', (r) => {
      if (r.status !== 'in_progress') {
        console.log(`[${USERNAME}] path_update status=${r.status} path_len=${r.path?.length ?? 0}`);
      }
    });
    bot.on('goal_reached', () => console.log(`[${USERNAME}] goal_reached`));
    bot.on('path_reset',   (r) => console.log(`[${USERNAME}] path_reset reason=${r}`));
    bot.on('path_stop',    ()  => console.log(`[${USERNAME}] path_stop`));

    switch (ROLE) {
      case 'wander': startWander(bot); break;
      case 'follow': startFollow(bot); break;
      case 'farm':   startFarm(bot);   break;
      case 'combat': startCombat(bot); break;
      default:
        console.warn(`[${USERNAME}] Unknown role "${ROLE}", defaulting to wander`);
        startWander(bot);
    }
  });

  bot.on('chat', (sender, message) => {
    if (sender === bot.username) return;
    handleChat(bot, sender, message).catch(err =>
      console.error(`[${bot.username}] Chat handler error:`, err.message)
    );
  });

  bot.on('error', (err) => {
    if (err.code !== 'ECONNREFUSED') {
      console.error(`[${USERNAME}] Error:`, err.message);
    }
  });

  bot.on('end', (reason) => {
    console.log(`[${USERNAME}] Disconnected (${reason}). Reconnecting in ${reconnectDelay / 1000}s…`);
    const delay = reconnectDelay;
    reconnectDelay = Math.min(reconnectDelay * 2, RECONNECT_MAX_MS);
    setTimeout(createBot, delay);
  });
}

// ── Chat handler ──────────────────────────────────────────────────────────────
async function handleChat(bot, sender, message) {
  const lower = message.toLowerCase().trim();

  if (lower.includes(bot.username.toLowerCase())) {
    bot.chat(`Hi ${sender}! I'm the ${ROLE} bot. Type !help for commands.`);
    return;
  }
  switch (true) {
    case lower === '!help':
      bot.chat('Commands: !come  !follow <name>  !stop  !attack <name>  !roles');
      break;
    case lower === '!roles':
      bot.chat('Roles: wander | follow | farm | combat');
      break;
    case lower === '!stop':
      bot.pathfinder.stop();
      bot.chat('Stopped.');
      break;
    case lower === '!come':
      await cmdFollow(bot, sender);
      break;
    case lower.startsWith('!follow '):
      await cmdFollow(bot, message.split(' ')[1]);
      break;
    case lower.startsWith('!attack '):
      cmdAttack(bot, message.split(' ')[1]);
      break;
  }
}

// ── Wander behavior ───────────────────────────────────────────────────────────
// Uses goto() (Promise-based) so each wander leg properly completes or
// times out before the next one begins. GoalXZ lets the pathfinder pick Y.
function startWander(bot) {
  const RADIUS = 48;
  const QUOTES = [
    'Just exploring!', 'Have you tried punching a tree?', 'Creepers? Aww man.',
    'Looking for diamonds…', 'This is fine. 🔥', 'Found any good caves lately?',
    'I love this place.', 'Watch out for Endermen!',
  ];

  async function wanderLoop() {
    while (true) {
      if (!bot.entity) { await sleep(2000); continue; }

      const x = Math.floor(bot.entity.position.x + (Math.random() - 0.5) * RADIUS * 2);
      const z = Math.floor(bot.entity.position.z + (Math.random() - 0.5) * RADIUS * 2);

      console.log(`[${bot.username}] Wandering to ${x}, ${z}`);
      try {
        await bot.pathfinder.goto(new GoalXZ(x, z));
        console.log(`[${bot.username}] Reached wander target`);
      } catch (err) {
        console.log(`[${bot.username}] Wander goto error: name=${err.name} msg=${err.message}`);
      }
      await sleep(1000 + Math.random() * 4000);
    }
  }

  // Ambient chat on a separate timer
  setInterval(() => {
    if (bot.entity) {
      bot.chat(QUOTES[Math.floor(Math.random() * QUOTES.length)]);
    }
  }, 3 * 60_000);

  wanderLoop().catch(err => console.error(`[${bot.username}] Wander loop died:`, err.message));
}

// ── Follow/Companion behavior ─────────────────────────────────────────────────
// Follows nearest player and actively:
//  • Defends against nearby hostiles
//  • Picks up nearby dropped loot
//  • Places torches in dark areas
//  • Warns about nearby dangers in chat
function startFollow(bot) {
  const HOSTILE = new Set([
    'zombie', 'skeleton', 'spider', 'cave_spider', 'creeper', 'enderman',
    'witch', 'pillager', 'vindicator', 'evoker', 'phantom', 'drowned',
    'husk', 'stray', 'slime', 'magma_cube', 'blaze', 'ghast',
    'wither_skeleton', 'zombified_piglin', 'hoglin', 'piglin_brute', 'ravager',
  ]);

  let defending = false;
  let lastDangerWarn = 0;
  let lastHealthWarn = {};

  // ── Pathfind to nearest player ──────────────────────────────────────────
  setInterval(() => {
    if (!bot.entity || defending) return;

    let nearest = null;
    let nearestDist = Infinity;
    for (const p of Object.values(bot.players)) {
      if (!p.entity || p.username === bot.username) continue;
      const d = bot.entity.position.distanceTo(p.entity.position);
      if (d < nearestDist) { nearestDist = d; nearest = p; }
    }

    if (!nearest) { bot.pathfinder.stop(); return; }

    if (nearestDist > 4) {
      bot.pathfinder.setGoal(new GoalFollow(nearest.entity, 3), true);
    } else {
      bot.pathfinder.stop();
    }
  }, 1000);

  // ── Defend: attack hostiles within 10 blocks of any player ─────────────
  bot.on('physicsTick', () => {
    if (defending || !bot.entity) return;

    // Find hostile mob near any human player
    let target = null;
    let targetDist = Infinity;
    for (const e of Object.values(bot.entities)) {
      if (e.type !== 'mob' || !e.name || !HOSTILE.has(e.name.toLowerCase())) continue;
      // Check if it's close to the bot or any player
      const distToBot = bot.entity.position.distanceTo(e.position);
      if (distToBot < 12 && distToBot < targetDist) {
        targetDist = distToBot;
        target = e;
      }
    }
    if (!target) return;

    defending = true;
    bot.pathfinder.stop();
    fightMob(bot, target, HOSTILE)
      .catch(err => console.warn(`[${bot.username}] Defend error: ${err.message}`))
      .finally(() => { defending = false; });
  });

  // ── Loot: pick up nearby dropped items ─────────────────────────────────
  setInterval(() => {
    if (defending || !bot.entity) return;
    const item = bot.nearestEntity(e =>
      e.name === 'item' &&
      bot.entity.position.distanceTo(e.position) < 6
    );
    if (item) {
      bot.pathfinder.setGoal(new GoalNear(
        item.position.x, item.position.y, item.position.z, 1
      ), true);
    }
  }, 2000);

  // ── Torch: place torch if standing in dark spot ─────────────────────────
  setInterval(async () => {
    if (defending || !bot.entity) return;
    const light = bot.world.getLight(bot.entity.position.floored());
    if (light > 7) return; // bright enough

    const torch = bot.inventory.items().find(i =>
      i.name === 'torch' || i.name === 'soul_torch'
    );
    if (!torch) return;

    // Find a solid block below feet to place torch on
    const below = bot.blockAt(bot.entity.position.offset(0, -1, 0));
    if (!below || below.name === 'air') return;

    try {
      await bot.equip(torch, 'hand');
      await bot.placeBlock(below, new (require('vec3'))(0, 1, 0));
    } catch (_) { /* surface may not be valid */ }
  }, 5000);

  // ── Danger warnings: alert players to nearby threats ───────────────────
  setInterval(() => {
    if (!bot.entity) return;
    const now = Date.now();
    if (now - lastDangerWarn < 15_000) return; // throttle warnings to every 15s

    // Mob danger
    const nearMob = bot.nearestEntity(e =>
      e.type === 'mob' && !!e.name && HOSTILE.has(e.name.toLowerCase()) &&
      bot.entity.position.distanceTo(e.position) < 16
    );
    if (nearMob) {
      bot.chat(`⚠ ${nearMob.name} nearby!`);
      lastDangerWarn = now;
      return;
    }

    // Lava/fire danger
    const DANGER_BLOCKS = new Set(['lava', 'flowing_lava', 'fire']);
    const dangerBlock = bot.findBlock({
      matching: b => DANGER_BLOCKS.has(b.name),
      maxDistance: 5,
    });
    if (dangerBlock) {
      bot.chat('⚠ Danger: lava or fire nearby!');
      lastDangerWarn = now;
      return;
    }

    // Player low health warning
    for (const p of Object.values(bot.players)) {
      if (!p.entity || p.username === bot.username) continue;
      // bot can only see its own health via bot.health; player health not exposed
      // Instead warn if bot itself is low
    }
  }, 3000);

  // ── Self health warning ─────────────────────────────────────────────────
  bot.on('health', () => {
    if (bot.health <= 6) {
      bot.chat(`I'm hurt! Health: ${bot.health.toFixed(1)} ❤`);
    }
  });
}

// ── Farm behavior ─────────────────────────────────────────────────────────────
// Uses getProperties().age for crop maturity (block.metadata removed in 1.13+).
// GoalGetToBlock positions the bot adjacent to (not inside) the crop.
function startFarm(bot) {
  // Max age for each crop type
  const CROP_MAX_AGE = { wheat: 7, carrots: 7, potatoes: 7, beetroots: 3 };

  async function farmLoop() {
    while (true) {
      if (!bot.entity) { await sleep(2000); continue; }

      const mcData = require('minecraft-data')(bot.version);
      const cropIds = Object.keys(CROP_MAX_AGE)
        .map(c => mcData.blocksByName[c]?.id)
        .filter(Boolean);

      const cropPos = bot.findBlock({
        matching: cropIds,
        maxDistance: 32,
        useExtraInfo: (block) => {
          const maxAge = CROP_MAX_AGE[block.name] ?? 7;
          const props  = block.getProperties();
          return parseInt(props?.age ?? '0', 10) >= maxAge;
        },
      });

      if (!cropPos) { await sleep(5000); continue; }

      try {
        await bot.pathfinder.goto(
          new GoalGetToBlock(cropPos.position.x, cropPos.position.y, cropPos.position.z)
        );

        // Re-fetch block — someone else may have harvested it while we walked over
        const block = bot.blockAt(cropPos.position);
        if (block && block.name !== 'air' && block.diggable) {
          const props  = block.getProperties();
          const maxAge = CROP_MAX_AGE[block.name] ?? 7;
          if (parseInt(props?.age ?? '0', 10) >= maxAge) {
            await bot.dig(block, true); // forceLook=true snaps head instantly
          }
        }
      } catch (err) {
        console.warn(`[${bot.username}] Farm error: ${err.message}`);
      }

      await sleep(500);
    }
  }

  farmLoop().catch(err => console.error(`[${bot.username}] Farm loop died:`, err.message));
}

// ── Combat behavior ───────────────────────────────────────────────────────────
// Driven by physicsTick (every game tick ~50ms) for responsive detection.
// Uses bot.nearestEntity() for efficient hostile lookup.
// Equips best weapon, looks at mob before attacking for server-side hit registration.
function startCombat(bot) {
  const HOSTILE = new Set([
    'zombie', 'skeleton', 'spider', 'cave_spider', 'creeper', 'enderman',
    'witch', 'pillager', 'vindicator', 'evoker', 'phantom', 'drowned',
    'husk', 'stray', 'slime', 'magma_cube', 'blaze', 'ghast',
    'wither_skeleton', 'zombified_piglin', 'hoglin', 'piglin_brute', 'ravager',
  ]);

  let combatActive = false;

  bot.on('physicsTick', () => {
    if (combatActive || !bot.entity) return;

    const mob = bot.nearestEntity(e =>
      e.type === 'mob' &&
      !!e.name &&
      HOSTILE.has(e.name.toLowerCase()) &&
      bot.entity.position.distanceTo(e.position) <= 20
    );

    if (!mob) return;

    combatActive = true;
    fightMob(bot, mob, HOSTILE)
      .catch(err => console.warn(`[${bot.username}] Combat error: ${err.message}`))
      .finally(() => { combatActive = false; });
  });
}

async function fightMob(bot, mob, HOSTILE) {
  // Equip best melee weapon
  const weapon = bot.inventory.items().find(i =>
    i.name.includes('sword') || i.name.includes('axe')
  );
  if (weapon) await bot.equip(weapon, 'hand');

  // Navigate to melee range
  try {
    await bot.pathfinder.goto(
      new GoalNear(mob.position.x, mob.position.y, mob.position.z, 2)
    );
  } catch (err) {
    return; // can't reach — give up and let next tick try again
  }

  // Attack loop until mob dies or leaves range
  while (true) {
    const liveMob = bot.entities[mob.id];
    if (!liveMob || !HOSTILE.has(liveMob.name?.toLowerCase())) break;

    const dist = bot.entity.position.distanceTo(liveMob.position);
    if (dist > 20) break; // mob ran away

    if (dist > 3) {
      // Re-close the gap
      try {
        await bot.pathfinder.goto(
          new GoalNear(liveMob.position.x, liveMob.position.y, liveMob.position.z, 2)
        );
      } catch { break; }
    }

    // Look at mob center before swinging — ensures server-side hit registration
    await bot.lookAt(liveMob.position.offset(0, liveMob.height * 0.5, 0));
    bot.attack(liveMob);

    // Wait for attack cooldown (sword = 625ms, use 650ms for safety)
    await sleep(650);
  }
}

// ── Command helpers ───────────────────────────────────────────────────────────
async function cmdFollow(bot, targetName) {
  const player = bot.players[targetName];
  if (!player?.entity) {
    bot.chat(`Can't find player "${targetName}".`);
    return;
  }
  bot.pathfinder.setGoal(new GoalFollow(player.entity, 3), true);
  bot.chat(`Following ${targetName}!`);
}

function cmdAttack(bot, targetName) {
  const entity = Object.values(bot.entities).find(
    e => e.username === targetName || e.name === targetName
  );
  if (!entity) {
    bot.chat(`Can't find "${targetName}".`);
    return;
  }
  bot.attack(entity);
  bot.chat(`Attacking ${targetName}!`);
}

// ── Helpers ───────────────────────────────────────────────────────────────────
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));

// ── Start ─────────────────────────────────────────────────────────────────────
createBot();

