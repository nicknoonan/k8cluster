'use strict';

const mineflayer = require('mineflayer');
const { pathfinder, Movements, goals } = require('mineflayer-pathfinder');
const { GoalNear, GoalFollow, GoalBlock } = goals;

// ── Configuration ────────────────────────────────────────────────────────────
const HOST        = process.env.MC_HOST     || 'minecraft.minecraft.svc.cluster.local';
const PORT        = parseInt(process.env.MC_PORT || '25565', 10);
const USERNAME    = process.env.BOT_USERNAME || 'Bot';
const ROLE        = (process.env.BOT_ROLE   || 'wander').toLowerCase();
const MC_VERSION  = process.env.MC_VERSION  || '1.21.4';
const RECONNECT_BASE_MS  = 10_000;
const RECONNECT_MAX_MS   = 120_000;
let   reconnectDelay     = RECONNECT_BASE_MS;

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
    reconnectDelay = RECONNECT_BASE_MS; // reset backoff on successful connect
    console.log(`[${USERNAME}] Spawned. Role: ${ROLE}`);
    const mcData    = require('minecraft-data')(bot.version);
    const movements = new Movements(bot, mcData);
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

  bot.on('error',      (err) => console.error(`[${USERNAME}] Error:`, err.message));
  bot.on('end',        (reason) => {
    console.log(`[${USERNAME}] Disconnected (${reason}). Reconnecting in ${reconnectDelay / 1000}s…`);
    setTimeout(createBot, reconnectDelay);
    reconnectDelay = Math.min(reconnectDelay * 2, RECONNECT_MAX_MS);
  });
}

// ── Chat handler (role-aware responses) ──────────────────────────────────────
function handleChat(bot, sender, message) {
  const lower = message.toLowerCase();
  if (lower.includes(bot.username.toLowerCase())) {
    bot.chat(`Hi ${sender}! I'm a ${ROLE} bot. Use "!help" for commands.`);
  }
  if (lower === '!help') {
    bot.chat(`Commands: !follow <name>, !stop, !attack <name>, !farm`);
  }
  if (lower.startsWith('!follow ')) {
    const target = lower.split(' ')[1];
    cmdFollow(bot, target);
  }
  if (lower === '!stop') {
    bot.pathfinder.stop();
    bot.chat('Stopped.');
  }
  if (lower.startsWith('!attack ')) {
    const target = lower.split(' ')[1];
    cmdAttack(bot, target);
  }
  if (lower === '!farm') {
    startFarm(bot, require('minecraft-data')(bot.version), bot.pathfinder.movements);
  }
}

// ── Wander behavior ───────────────────────────────────────────────────────────
function startWander(bot, mcData, movements) {
  function wander() {
    const x = bot.entity.position.x + (Math.random() - 0.5) * 40;
    const z = bot.entity.position.z + (Math.random() - 0.5) * 40;
    const goal = new GoalNear(x, bot.entity.position.y, z, 1);
    bot.pathfinder.setGoal(goal);
    bot.pathfinder.on('goal_reached', () => {
      bot.pathfinder.removeAllListeners('goal_reached');
      const delay = 5000 + Math.random() * 10000;
      setTimeout(wander, delay);
    });
  }
  wander();
  setInterval(() => {
    const jokes = [
      'Mining away!', 'Have you tried punching a tree?', 'Creepers? Aww man.',
      'Looking for diamonds…', 'This is fine. 🔥',
    ];
    bot.chat(jokes[Math.floor(Math.random() * jokes.length)]);
  }, 120_000);
}

// ── Follow behavior ───────────────────────────────────────────────────────────
function startFollow(bot, mcData, movements) {
  setInterval(() => {
    const players = Object.values(bot.players).filter(p => p.entity && p.username !== USERNAME);
    if (players.length === 0) return;
    const nearest = players.reduce((a, b) =>
      bot.entity.position.distanceTo(a.entity.position) <
      bot.entity.position.distanceTo(b.entity.position) ? a : b
    );
    bot.pathfinder.setGoal(new GoalFollow(nearest.entity, 3), true);
  }, 1000);
}

// ── Farm behavior ─────────────────────────────────────────────────────────────
function startFarm(bot, mcData, movements) {
  const CROPS   = ['wheat', 'carrots', 'potatoes', 'beetroots'];
  const cropIds = CROPS.map(c => mcData.blocksByName[c]?.id).filter(Boolean);

  async function farmLoop() {
    // Harvest ripe crops
    const crop = bot.findBlock({ matching: cropIds, maxDistance: 32 });
    if (crop) {
      const block = bot.blockAt(crop.position);
      if (block && block.metadata === 7) { // fully grown
        try {
          await bot.pathfinder.goto(new GoalBlock(crop.position.x, crop.position.y, crop.position.z));
          await bot.dig(block);
        } catch (_) { /* ignore nav errors */ }
      }
    }
    setTimeout(farmLoop, 3000);
  }
  farmLoop();
}

// ── Combat behavior ───────────────────────────────────────────────────────────
function startCombat(bot, mcData, movements) {
  const HOSTILE = new Set([
    'zombie', 'skeleton', 'spider', 'creeper', 'enderman',
    'witch', 'pillager', 'vindicator', 'phantom', 'drowned',
  ]);

  setInterval(() => {
    const mob = Object.values(bot.entities).find(e =>
      e.type === 'mob' && HOSTILE.has(e.name) &&
      bot.entity.position.distanceTo(e.position) < 16
    );
    if (mob) {
      bot.pathfinder.setGoal(new GoalNear(mob.position.x, mob.position.y, mob.position.z, 2), true);
      bot.attack(mob);
    }
  }, 500);
}

// ── Command helpers ───────────────────────────────────────────────────────────
function cmdFollow(bot, targetName) {
  const player = bot.players[targetName];
  if (!player?.entity) { bot.chat(`Can't find ${targetName}.`); return; }
  bot.pathfinder.setGoal(new GoalFollow(player.entity, 3), true);
  bot.chat(`Following ${targetName}!`);
}

function cmdAttack(bot, targetName) {
  const entity = Object.values(bot.entities).find(e => e.username === targetName || e.name === targetName);
  if (!entity) { bot.chat(`Can't find ${targetName}.`); return; }
  bot.attack(entity);
  bot.chat(`Attacking ${targetName}!`);
}

// ── Start ─────────────────────────────────────────────────────────────────────
createBot();
