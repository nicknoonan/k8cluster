'use strict';

const mineflayer = require('mineflayer');
const { pathfinder, Movements, goals } = require('mineflayer-pathfinder');
const { GoalNear, GoalFollow, GoalBlock, GoalXZ } = goals;

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

    const mcData    = require('minecraft-data')(bot.version);
    const movements = new Movements(bot);
    movements.allowSprinting = true;
    movements.canDig = false; // bots shouldn't mine player builds
    bot.pathfinder.setMovements(movements);

    switch (ROLE) {
      case 'wander':   startWander(bot, mcData, movements); break;
      case 'follow':   startFollow(bot, mcData, movements); break;
      case 'farm':     startFarm(bot, mcData, movements);   break;
      case 'combat':   startCombat(bot, mcData, movements); break;
      default:
        console.warn(`[${USERNAME}] Unknown role "${ROLE}", defaulting to wander`);
        startWander(bot, mcData, movements);
    }
  });

  bot.on('chat', (sender, message) => {
    if (sender === USERNAME) return;
    handleChat(bot, sender, message);
  });

  bot.on('error', (err) => {
    // Suppress noisy ECONNREFUSED during server startup
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
function handleChat(bot, sender, message) {
  const lower = message.toLowerCase().trim();

  if (lower.includes(bot.username.toLowerCase())) {
    bot.chat(`Hi ${sender}! I'm the ${ROLE} bot. Type !help for commands.`);
    return;
  }
  if (lower === '!help') {
    bot.chat('Commands: !follow <name>  !stop  !attack <name>  !come  !roles');
    return;
  }
  if (lower === '!roles') {
    bot.chat('Roles: wander | follow | farm | combat');
    return;
  }
  if (lower === '!stop') {
    bot.pathfinder.stop();
    bot.chat('Stopped.');
    return;
  }
  if (lower === '!come') {
    cmdFollow(bot, sender);
    return;
  }
  if (lower.startsWith('!follow ')) {
    cmdFollow(bot, message.split(' ')[1]);
    return;
  }
  if (lower.startsWith('!attack ')) {
    cmdAttack(bot, message.split(' ')[1]);
    return;
  }
}

// ── Wander behavior ───────────────────────────────────────────────────────────
// Uses GoalXZ so pathfinder finds a valid surface Y automatically.
// Polls on an interval rather than listening to goal_reached to avoid
// listener accumulation when goals fail or time out.
function startWander(bot, mcData, movements) {
  const WANDER_RADIUS = 48;
  const IDLE_QUOTES = [
    'Just exploring!', 'Have you tried punching a tree?', 'Creepers? Aww man.',
    'Looking for diamonds…', 'This is fine. 🔥', 'Found any good caves lately?',
    'I love this place.', 'Watch out for Endermen!',
  ];

  let wandering = false;

  function pickNextWander() {
    if (wandering) return;
    if (!bot.entity) return;
    wandering = true;

    const x = Math.floor(bot.entity.position.x + (Math.random() - 0.5) * WANDER_RADIUS * 2);
    const z = Math.floor(bot.entity.position.z + (Math.random() - 0.5) * WANDER_RADIUS * 2);

    bot.pathfinder.setGoal(new GoalXZ(x, z));
  }

  // Check periodically; if not moving, pick a new destination
  setInterval(() => {
    if (!bot.entity) return;
    const vel = bot.entity.velocity;
    const isMoving = Math.abs(vel.x) + Math.abs(vel.z) > 0.01;
    if (!isMoving) {
      wandering = false;
      pickNextWander();
    }
  }, 3000);

  // Ambient chat
  setInterval(() => {
    if (!bot.entity) return;
    bot.chat(IDLE_QUOTES[Math.floor(Math.random() * IDLE_QUOTES.length)]);
  }, 3 * 60_000);

  pickNextWander();
}

// ── Follow behavior ───────────────────────────────────────────────────────────
// Follows the nearest player, stays 3 blocks away.
// Guards against null entity references which crash older code.
function startFollow(bot, mcData, movements) {
  setInterval(() => {
    if (!bot.entity) return;

    const players = Object.values(bot.players).filter(
      p => p.entity && p.username !== bot.username
    );
    if (players.length === 0) {
      bot.pathfinder.stop();
      return;
    }

    let nearest = null;
    let nearestDist = Infinity;
    for (const p of players) {
      const d = bot.entity.position.distanceTo(p.entity.position);
      if (d < nearestDist) { nearestDist = d; nearest = p; }
    }

    if (nearest && nearestDist > 4) {
      bot.pathfinder.setGoal(new GoalFollow(nearest.entity, 3), true);
    } else if (nearest && nearestDist <= 4) {
      bot.pathfinder.stop();
    }
  }, 1000);
}

// ── Farm behavior ─────────────────────────────────────────────────────────────
// In 1.13+ block properties replace metadata. Age 7 = fully grown for
// wheat/carrots/potatoes/beetroots (beetroot is age 3 for fully grown).
function startFarm(bot, mcData, movements) {
  const CROP_MAX_AGE = { wheat: 7, carrots: 7, potatoes: 7, beetroots: 3 };
  const cropIds = Object.keys(CROP_MAX_AGE)
    .map(c => mcData.blocksByName[c]?.id)
    .filter(Boolean);

  let farming = false;

  async function farmLoop() {
    if (farming || !bot.entity) {
      setTimeout(farmLoop, 2000);
      return;
    }

    const cropBlock = bot.findBlock({
      matching: cropIds,
      maxDistance: 32,
      useExtraInfo: (block) => {
        const name = block.name;
        const maxAge = CROP_MAX_AGE[name] ?? 7;
        const props  = block.getProperties();
        return parseInt(props.age ?? '0', 10) >= maxAge;
      },
    });

    if (!cropBlock) {
      setTimeout(farmLoop, 5000);
      return;
    }

    farming = true;
    try {
      await bot.pathfinder.goto(new GoalBlock(
        cropBlock.position.x,
        cropBlock.position.y,
        cropBlock.position.z,
      ));
      // Re-check after navigating in case another bot or player harvested it
      const fresh = bot.blockAt(cropBlock.position);
      if (fresh) {
        const props  = fresh.getProperties();
        const maxAge = CROP_MAX_AGE[fresh.name] ?? 7;
        if (parseInt(props.age ?? '0', 10) >= maxAge) {
          await bot.dig(fresh);
        }
      }
    } catch (err) {
      console.warn(`[${bot.username}] Farm nav error: ${err.message}`);
    } finally {
      farming = false;
    }

    setTimeout(farmLoop, 1000);
  }

  farmLoop();
}

// ── Combat behavior ───────────────────────────────────────────────────────────
// Finds the nearest hostile mob, navigates close, then attacks.
// Checks mob still exists before each attack to avoid crashes on mob death.
function startCombat(bot, mcData, movements) {
  const HOSTILE = new Set([
    'zombie', 'skeleton', 'spider', 'cave_spider', 'creeper', 'enderman',
    'witch', 'pillager', 'vindicator', 'evoker', 'phantom', 'drowned',
    'husk', 'stray', 'slime', 'magma_cube', 'blaze', 'ghast',
  ]);

  let attacking = false;

  setInterval(() => {
    if (!bot.entity) return;

    // Find nearest hostile mob within 20 blocks
    let target = null;
    let targetDist = Infinity;
    for (const e of Object.values(bot.entities)) {
      if (e === bot.entity) continue;
      if (e.type !== 'mob') continue;
      if (!HOSTILE.has(e.name)) continue;
      const d = bot.entity.position.distanceTo(e.position);
      if (d < 20 && d < targetDist) { targetDist = d; target = e; }
    }

    if (!target) {
      if (attacking) { bot.pathfinder.stop(); attacking = false; }
      return;
    }

    attacking = true;

    // Navigate to within sword range
    bot.pathfinder.setGoal(new GoalNear(
      target.position.x, target.position.y, target.position.z, 2
    ), true);

    // Attack if in melee range
    if (targetDist <= 3 && bot.entities[target.id]) {
      bot.attack(target);
    }
  }, 400);
}

// ── Command helpers ───────────────────────────────────────────────────────────
function cmdFollow(bot, targetName) {
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

// ── Start ─────────────────────────────────────────────────────────────────────
createBot();
