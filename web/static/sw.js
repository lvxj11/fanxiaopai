/* 离线缓存：装到主屏幕后，断网也能看到上一份菜单和采购清单。
 *
 * 只有 HTTPS（或 localhost）下浏览器才会注册这个文件 —— 局域网
 * http://内网IP 不是 secure context，注册代码会静默跳过，不影响现有用法。
 *
 * 改了 web/static/app.css 之后把下面的 CACHE 版本号 +1，否则用户看到的还是旧样式。
 */
var CACHE = 'fanxiaopai-v2';
var SHELL = [
  '/menu',
  '/shopping',
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

self.addEventListener('fetch', function (event) {
  var request = event.request;
  if (request.method !== 'GET') return;

  var url = new URL(request.url);
  if (url.origin !== self.location.origin) return;

  // 菜单与采购清单：先给缓存里的那份，同时后台去取新的
  if (url.pathname === '/api/menu' || url.pathname === '/api/shopping') {
    event.respondWith(staleWhileRevalidate(request));
    return;
  }

  // 页面本身：有网就走网络，断网才回落到缓存
  if (request.mode === 'navigate') {
    event.respondWith(networkFirst(request));
    return;
  }

  // 静态资源直接用缓存
  if (url.pathname.indexOf('/static/') === 0) {
    event.respondWith(cacheFirst(request));
  }
});

function networkFirst(request) {
  return fetch(request).then(function (response) {
    return caches.open(CACHE).then(function (cache) {
      cache.put(request, response.clone());
      return response;
    });
  }).catch(function () {
    return caches.match(request).then(function (hit) {
      return hit || caches.match('/menu');
    });
  });
}

function staleWhileRevalidate(request) {
  return caches.open(CACHE).then(function (cache) {
    return cache.match(request).then(function (hit) {
      var fresh = fetch(request).then(function (response) {
        if (response.ok) cache.put(request, response.clone());
        return response;
      }).catch(function () {
        return hit;
      });
      return hit || fresh;
    });
  });
}

function cacheFirst(request) {
  return caches.match(request).then(function (hit) {
    if (hit) return hit;
    return fetch(request).then(function (response) {
      return caches.open(CACHE).then(function (cache) {
        cache.put(request, response.clone());
        return response;
      });
    });
  });
}
