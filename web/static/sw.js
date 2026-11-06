/* 离线缓存：装到主屏幕后，断网也能看到上一份菜单和采购清单。
 *
 * 只有 HTTPS（或 localhost）下浏览器才会注册这个文件 —— 局域网
 * http://内网IP 不是 secure context，注册代码会静默跳过，不影响现有用法。
 *
 * 改了 web/static/app.css 之后把下面的 CACHE 版本号 +1，否则用户看到的还是旧样式。
 *
 * 鉴权后的改动（v4）：
 *   - 绝不缓存登录/设置页、登录重定向、401/403 响应；
 *   - 只缓存静态资源和「已登录时」200 的菜单/采购 JSON；
 *   - 页面导航只有「同源、200、未重定向」才进缓存，断网回落到缓存或 /login；
 *   - 收到页面发来的 logout 消息时清空所有缓存，避免私有数据留在设备上。
 */
var CACHE = 'fanxiaopai-v6';

// 只预缓存与账号无关的静态资源 + 中性的 /login 兜底页；不再缓存 /menu、/shopping。
// /login 在已登录时会 302 到 /menu，用 credentials:'omit' 让预缓存不带会话 Cookie，
// 确保缓存的是中性的登录表单，而不是登录态下的菜单骨架（私有页面）。
var SHELL = [
  new Request('/login', { credentials: 'omit' }),
  '/static/app.css',
  '/static/alpine.min.js',
  '/static/manifest.webmanifest',
  '/static/icon-180.png',
  '/static/icon-192.png',
  '/static/icon-512.png'
];

self.addEventListener('install', function (event) {
  event.waitUntil(
    caches.open(CACHE)
      .then(function (cache) { return cache.addAll(SHELL); })
      .then(function () { return self.skipWaiting(); })
  );
});

self.addEventListener('activate', function (event) {
  event.waitUntil(
    caches.keys()
      .then(function (keys) {
        return Promise.all(keys.filter(function (k) {
          return k !== CACHE;
        }).map(function (k) {
          return caches.delete(k);
        }));
      })
      .then(function () { return self.clients.claim(); })
  );
});

// 退出登录时清空缓存：把菜单/采购这类私有数据从设备上抹掉
self.addEventListener('message', function (event) {
  if (event.data && event.data.type === 'logout') {
    event.waitUntil(
      caches.keys().then(function (keys) {
        return Promise.all(keys.map(function (k) { return caches.delete(k); }));
      })
    );
  }
});

self.addEventListener('fetch', function (event) {
  var request = event.request;
  if (request.method !== 'GET') return;

  var url = new URL(request.url);
  if (url.origin !== self.location.origin) return;

  var path = url.pathname;

  // 这些请求一律走网络、绝不缓存：登录/设置页、离线快照页，以及除下面两个 GET 外的所有 /api/*
  if (path === '/login' || path === '/setup' || path === '/export') return;
  if (path.indexOf('/api/') === 0 && path !== '/api/menu' && path !== '/api/shopping') return;

  // 菜单与采购清单 GET：网络优先，只缓存 200；401/403 时删缓存并把响应交回页面去跳登录
  if (path === '/api/menu' || path === '/api/shopping') {
    event.respondWith(apiNetworkFirst(request));
    return;
  }

  // 页面导航：有网走网络，断网才回落到缓存
  if (request.mode === 'navigate') {
    event.respondWith(networkFirst(request));
    return;
  }

  // 静态资源直接用缓存
  if (path.indexOf('/static/') === 0) {
    event.respondWith(cacheFirst(request));
  }
});

function isCacheable(response) {
  return response && response.ok && !response.redirected && response.type === 'basic';
}

function networkFirst(request) {
  return fetch(request).then(function (response) {
    // 重定向到 /login 的响应 response.ok 仍为 true，必须靠 redirected/basic 过滤，
    // 否则登录页会被缓存进 /menu，退出后还能看到旧菜单骨架
    if (!isCacheable(response)) return response;
    return caches.open(CACHE).then(function (cache) {
      cache.put(request, response.clone());
      return response;
    });
  }).catch(function () {
    return caches.match(request).then(function (hit) {
      return hit || caches.match('/login');
    });
  });
}

function apiNetworkFirst(request) {
  return fetch(request).then(function (response) {
    if (response.status === 401 || response.status === 403 || response.redirected) {
      // 会话失效：清掉这份缓存，别再吐旧的私有数据；响应原样交回，前端 window.api 会跳登录
      return caches.open(CACHE).then(function (cache) {
        return cache.delete(request).then(function () { return response; });
      });
    }
    if (response.status === 200 && !response.redirected && response.type === 'basic') {
      return caches.open(CACHE).then(function (cache) {
        cache.put(request, response.clone());
        return response;
      });
    }
    return response;
  }).catch(function () {
    // 断网：回吐自己设备上已看过的那份（用户自有缓存数据，可接受）
    return caches.match(request).then(function (hit) {
      return hit || new Response('{"error":"离线且无缓存"}', {
        status: 503,
        headers: { 'Content-Type': 'application/json; charset=utf-8' }
      });
    });
  });
}

function cacheFirst(request) {
  return caches.match(request).then(function (hit) {
    if (hit) return hit;
    return fetch(request).then(function (response) {
      if (!isCacheable(response)) return response;
      return caches.open(CACHE).then(function (cache) {
        cache.put(request, response.clone());
        return response;
      });
    });
  });
}
