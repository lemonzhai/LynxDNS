'use strict';
'require view';
'require ui';
'require uci';

var apiBase;
var concurrencyInput, idleTimeoutInput, cacheSizeInput, leakModeSelect, logLevelSelect, logFileInput;
var listenAddrInput, listenPortInput, apiAddrInput, apiPortInput, apiSecretInput;

function apiCall(method, path, data) {
	return new Promise(function(resolve) {
		var xhr = new XMLHttpRequest();
		xhr.open(method, apiBase + path, true);
		if (data) xhr.setRequestHeader('Content-Type', 'application/json');
		xhr.onreadystatechange = function() {
			if (xhr.readyState === 4) {
				try { resolve(JSON.parse(xhr.responseText || '{}')); }
				catch (e) { resolve({ code: 500, message: '解析错误' }); }
			}
		};
		xhr.send(data ? JSON.stringify(data) : null);
	});
}

function apiGet(p) { return apiCall('GET', p, null); }
function apiPatch(p, d) { return apiCall('PATCH', p, d); }
function apiDelete(p) { return apiCall('DELETE', p, null); }

function fieldRow(label, content, description) {
	var children = (typeof content === 'string') ? [E('span', {}, content)] : [content];
	if (description) children.push(E('div', { 'class': 'cbi-value-description' }, description));
	return E('div', { 'class': 'cbi-value' }, [
		E('label', { 'class': 'cbi-value-title' }, label),
		E('div', { 'class': 'cbi-value-field' }, children)
	]);
}

function showToast(msg) {
	var existing = document.querySelectorAll('.lynxdns-toast');
	existing.forEach(function(el) { el.remove(); });
	var t = E('div', { 'class': 'lynxdns-toast' }, msg);
	document.body.appendChild(t);
	setTimeout(function() { if (t.parentNode) t.remove(); }, 2500);
}

return view.extend({
	load: function() {
		apiBase = L.url('admin/services/lynxdns/api');
		return Promise.all([
			uci.load('lynxdns'),
			apiGet('/config').catch(function() { return { code: 500 }; }),
			apiGet('/ws_config').catch(function() { return { code: 500 }; })
		]);
	},

	render: function(data) {
		if (!document.querySelector('link[href*="lynxdns.css"]')) {
			document.head.appendChild(E('link', { 'rel': 'stylesheet', 'type': 'text/css', 'href': '/luci-static/lynxdns.css' }));
		}
		var config = (data[1] && data[1].code === 0) ? data[1].data : {};
		var wsCfg = (data[2] && data[2].code === 0) ? data[2].data : {};
		var adv = config.advanced || {};
		var cache = adv.cache || {};
		var leak = adv.leak_protection || {};
		var adFilter = adv.ad_filter || {};
		var log = config.log || {};

		var m = E('div', { 'class': 'cbi-map' }, [
			E('div', { 'class': 'cbi-map-descr' }, '调整 DNS 解析性能参数、缓存策略和安全防护设置。')
		]);

		var section = E('fieldset', { 'class': 'cbi-section' }, [
			E('legend', {}, '高级设置')
		]);

		var serverConfig = config.server || {};
		var serverFieldset = E('fieldset', { 'class': 'cbi-section', 'style': 'margin-bottom:16px' }, [
			E('legend', {}, 'DNS 服务设置'),
			E('h3', { 'style': 'font-size:14px;margin:0;padding:15px;border-bottom:1px solid #ddd;box-sizing:border-box;height:46px;border-bottom-left-radius:6px;border-bottom-right-radius:6px' }, 'LynxDNS 服务设置')
		]);
		listenAddrInput = E('input', {
			'type': 'text', 'class': 'cbi-input-text',
			'value': serverConfig.listen_addr || '0.0.0.0'
		});
		serverFieldset.appendChild(fieldRow('监听地址', listenAddrInput,
			'DNS 服务监听的网络接口地址，默认 0.0.0.0 表示监听所有接口'));
		listenPortInput = E('input', {
			'type': 'number', 'class': 'cbi-input-text',
			'value': serverConfig.listen_port || 5334,
			'min': '1', 'max': '65535'
		});
		serverFieldset.appendChild(fieldRow('监听端口', listenPortInput,
			'DNS 服务监听的端口号，默认 5334'));
		m.appendChild(serverFieldset);

		section.appendChild(E('h3', { 'style': 'font-size:14px;margin:0;padding:15px;border-bottom:1px solid #ddd;box-sizing:border-box;height:46px;border-bottom-left-radius:6px;border-bottom-right-radius:6px' }, '性能调优'));
		concurrencyInput = E('input', {
			'type': 'number', 'class': 'cbi-input-text',
			'value': adv.concurrency || 3, 'min': '1', 'max': '10'
		});
		section.appendChild(fieldRow('DNS 并发数', concurrencyInput,
			'同时向上游 DNS 服务器发起查询的数量，增大可提高响应速度但会增加负载。'));

		idleTimeoutInput = E('input', {
			'type': 'number', 'class': 'cbi-input-text',
			'value': adv.idle_timeout || 30, 'min': '5', 'max': '300'
		});
		section.appendChild(fieldRow('空闲超时 (秒)', idleTimeoutInput,
			'DoH/TCP/DoT 连接的保活时间，超时后自动断开空闲连接。'));

		section.appendChild(E('h3', { 'style': 'font-size:14px;margin:0;padding:15px;border-bottom:1px solid #ddd;box-sizing:border-box;height:46px;border-bottom-left-radius:6px;border-bottom-right-radius:6px' }, 'DNS 缓存'));
		var cacheEnabledSwitch = E('input', { 'type': 'checkbox', 'id': 'cb-cache' });
		if (cache.enabled !== false) cacheEnabledSwitch.checked = true;
		section.appendChild(fieldRow('启用缓存', cacheEnabledSwitch));

		cacheSizeInput = E('input', {
			'type': 'number', 'class': 'cbi-input-text',
			'value': cache.size || 4096, 'min': '256', 'max': '65536'
		});
		section.appendChild(fieldRow('缓存容量', cacheSizeInput,
			'DNS 缓存条目的最大数量，使用 LRU 算法淘汰过期条目。'));

		var lazySwitch = E('input', { 'type': 'checkbox', 'id': 'cb-lazy' });
		if (cache.lazy !== false) lazySwitch.checked = true;
		section.appendChild(fieldRow('惰性缓存', lazySwitch,
			'启用后，过期缓存结果会立即返回给客户端，同时在后台刷新。可提高响应速度但可能返回过期数据。'));

		var clearCacheBtn = E('button', {
			'class': 'cbi-button cbi-button-remove',
			'type': 'button'
		}, '清除缓存');
		clearCacheBtn.addEventListener('click', function() {
			if (confirm('确定要清除所有 DNS 缓存吗？')) {
				apiDelete('/dns_cache').then(function(resp) {
					var cleared = (resp.code === 0 && resp.data)
						? resp.data.cleared_entries || 0
						: 0;
					showToast('缓存已清除，共移除 ' + cleared + ' 条记录。');
				});
			}
		});
		section.appendChild(fieldRow('DNS 缓存', clearCacheBtn));

		section.appendChild(E('h3', { 'style': 'font-size:14px;margin:0;padding:15px;border-bottom:1px solid #ddd;box-sizing:border-box;height:46px;border-bottom-left-radius:6px;border-bottom-right-radius:6px' }, '广告拦截'));
		var adFilterEnabledSwitch = E('input', { 'type': 'checkbox', 'id': 'cb-adfilter' });
		if (adFilter.enabled !== false) adFilterEnabledSwitch.checked = true;
		section.appendChild(fieldRow('启用广告拦截', adFilterEnabledSwitch,
			'启用后，DNS 查询将匹配广告过滤规则（AdGuard 过滤规则和 GeoSite 广告分类域名），匹配的域名将被拦截。关闭后所有域名查询将正常通过。'));

		section.appendChild(E('h3', { 'style': 'font-size:14px;margin:0;padding:15px;border-bottom:1px solid #ddd;box-sizing:border-box;height:46px;border-bottom-left-radius:6px;border-bottom-right-radius:6px' }, 'DNS 防泄漏'));
		var leakEnabledSwitch = E('input', { 'type': 'checkbox', 'id': 'cb-leak' });
		if (leak.enabled !== false) leakEnabledSwitch.checked = true;
		section.appendChild(fieldRow('启用防泄漏', leakEnabledSwitch));

		leakModeSelect = E('select', { 'class': 'cbi-input-select' }, [
			E('option', { 'value': 'standard' }, '标准模式'),
			E('option', { 'value': 'strict' }, '严格模式'),
			E('option', { 'value': 'loose' }, '宽松模式')
		]);
		leakModeSelect.value = leak.mode || 'loose';
		section.appendChild(fieldRow('防护模式', leakModeSelect,
			'标准模式：匹配国外 → 远程 DNS，匹配国内 → 国内 DNS，未匹配任何 → 远程 DNS。严格模式：匹配国外 → 远程 DNS，匹配国内 → 国内 DNS，未匹配任何 → 拦截 (NXDOMAIN)。宽松模式：匹配国外 → 远程 DNS，匹配国内 → 国内 DNS，未匹配任何 → 默认 DNS'));

		var apiFieldset = E('fieldset', { 'class': 'cbi-section', 'style': 'margin-bottom:16px' }, [
			E('legend', {}, 'API 接口设置'),
			E('h3', { 'style': 'font-size:14px;margin:0;padding:15px;border-bottom:1px solid #ddd;box-sizing:border-box;height:46px;border-bottom-left-radius:6px;border-bottom-right-radius:6px' }, 'API 接口设置')
		]);
		var apiCfg = config.api || {};
		apiAddrInput = E('input', {
			'type': 'text', 'class': 'cbi-input-text',
			'value': apiCfg.addr || wsCfg.host || '127.0.0.1'
		});
		apiFieldset.appendChild(fieldRow('API 地址', apiAddrInput));
		apiPortInput = E('input', {
			'type': 'number', 'class': 'cbi-input-text',
			'value': apiCfg.port || wsCfg.port || '5335'
		});
		apiFieldset.appendChild(fieldRow('API 端口', apiPortInput));
		apiSecretInput = E('input', {
			'type': 'password', 'class': 'cbi-input-text',
			'value': apiCfg.secret || wsCfg.secret || ''
		});
		apiFieldset.appendChild(fieldRow('API 密钥', apiSecretInput,
			'首次启动时自动生成随机密钥。修改后将更新到核心服务和 UCI 配置，留空表示使用当前密钥。'));
		m.appendChild(apiFieldset);

		section.appendChild(E('h3', { 'style': 'font-size:14px;margin:0;padding:15px;border-bottom:1px solid #ddd;box-sizing:border-box;height:46px;border-bottom-left-radius:6px;border-bottom-right-radius:6px' }, '日志设置'));
		logLevelSelect = E('select', { 'class': 'cbi-input-select' }, [
			E('option', { 'value': 'debug' }, 'Debug（调试）'),
			E('option', { 'value': 'info' }, 'Info（信息）'),
			E('option', { 'value': 'warn' }, 'Warning（警告）'),
			E('option', { 'value': 'error' }, 'Error（错误）')
		]);
		logLevelSelect.value = ['debug', 'info', 'warn', 'error'].indexOf(log.level) >= 0 ? log.level : 'info';
		section.appendChild(fieldRow('日志级别', logLevelSelect,
			'日志输出的详细程度。Debug 输出最详细，Error 仅输出错误信息。'));

		logFileInput = E('input', {
			'type': 'text', 'class': 'cbi-input-text',
			'value': log.file || '/var/log/lynxdns.log',
			'placeholder': '/var/log/lynxdns.log'
		});
		section.appendChild(fieldRow('日志文件路径', logFileInput,
			'日志文件的保存路径。默认：/var/log/lynxdns.log'));

		var reloadBtn = E('button', {
			'class': 'cbi-button cbi-button-apply',
			'type': 'button'
		}, '重新加载');
		reloadBtn.addEventListener('click', function() {
			apiCall('POST', '/config_reload').then(function(resp) {
				showToast(resp.code === 0
						? '配置已从文件重新加载。'
						: '重新加载失败: ' + (resp.message || ''));
			});
		});
		section.appendChild(fieldRow('配置重载', reloadBtn));

		m.appendChild(section);

		return m;
	},

	handleSaveApply: function(ev) {
		var patchData = {
			server: {
				listen_addr: listenAddrInput.value,
				listen_port: parseInt(listenPortInput.value) || 5334
			},
			api: {
				addr: apiAddrInput.value,
				port: parseInt(apiPortInput.value) || 5335
			},
			advanced: {
				concurrency: parseInt(concurrencyInput.value) || 3,
				idle_timeout: parseInt(idleTimeoutInput.value) || 30,
				cache: {
					enabled: document.getElementById('cb-cache').checked,
					size: parseInt(cacheSizeInput.value) || 4096,
					lazy: document.getElementById('cb-lazy').checked
				},
				leak_protection: {
					enabled: document.getElementById('cb-leak').checked,
					mode: leakModeSelect.value
				},
				ad_filter: {
					enabled: document.getElementById('cb-adfilter').checked
				}
			},
			log: {
				level: logLevelSelect.value,
				file: logFileInput.value || '/var/log/lynxdns.log'
			}
		};
		apiPatch('/config', patchData).then(function(resp) {
			if (resp.code === 0) {
				uci.set('lynxdns', 'api', 'addr', apiAddrInput.value);
				uci.set('lynxdns', 'api', 'port', apiPortInput.value);
				uci.set('lynxdns', 'api', 'secret', apiSecretInput.value);
				uci.save();
				uci.apply();
				showToast('高级设置已保存并热加载完成。');
			} else {
				showToast('保存失败：' + (resp.message || '未知错误'));
			}
		});
	},

	handleSave: function(ev) {
		return this.handleSaveApply(ev);
	}
});
