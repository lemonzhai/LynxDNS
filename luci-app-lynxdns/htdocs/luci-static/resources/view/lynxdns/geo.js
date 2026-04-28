'use strict';
'require view';
'require ui';

var apiBase;
var progressTimer = null;
var cronInput, dataDirInput, geositeBackupEditor, geoipBackupEditor, adFilterBackupEditor;

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
function apiPost(p, d) { return apiCall('POST', p, d); }

function showToast(msg) {
	var existing = document.querySelectorAll('.lynxdns-toast');
	existing.forEach(function(el) { el.remove(); });
	var t = E('div', { 'class': 'lynxdns-toast' }, msg);
	document.body.appendChild(t);
	setTimeout(function() { if (t.parentNode) t.remove(); }, 2500);
}

function fieldRow(label, content, description) {
	var children = (typeof content === 'string') ? [E('span', {}, content)] : [content];
	if (description) children.push(E('div', { 'class': 'cbi-value-description' }, description));
	return E('div', { 'class': 'cbi-value' }, [
		E('label', { 'class': 'cbi-value-title' }, label),
		E('div', { 'class': 'cbi-value-field' }, children)
	]);
}

function formatBytes(bytes) {
	if (!bytes || bytes < 0) return 'N/A';
	if (bytes < 1024) return bytes + ' B';
	if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB';
	return (bytes / 1048576).toFixed(1) + ' MB';
}

function formatDate(iso) {
	if (!iso) return '无';
	try { return new Date(iso).toLocaleString('zh-CN'); }
	catch (e) { return iso; }
}

function formatSpeed(bps) {
	if (!bps || bps <= 0) return 'N/A';
	if (bps < 1024) return bps + ' B/s';
	if (bps < 1048576) return (bps / 1024).toFixed(1) + ' KB/s';
	return (bps / 1048576).toFixed(1) + ' MB/s';
}

function formatDuration(ms) {
	if (!ms || ms <= 0) return 'N/A';
	if (ms < 1000) return ms + ' ms';
	return (ms / 1000).toFixed(1) + ' s';
}

var geositePresets = [
	{ label: 'Loyalsoldier (GitHub)', value: 'https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat' },
	{ label: 'Loyalsoldier (jsdelivr)', value: 'https://cdn.jsdelivr.net/gh/Loyalsoldier/v2ray-rules-dat@release/geosite.dat' },
	{ label: 'v2fly (GitHub)', value: 'https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat' },
	{ label: 'Loyalsoldier (fastly)', value: 'https://fastly.jsdelivr.net/gh/Loyalsoldier/v2ray-rules-dat@release/geosite.dat' }
];

var geoipPresets = [
	{ label: 'Loyalsoldier (GitHub)', value: 'https://github.com/Loyalsoldier/geoip/releases/latest/download/geoip.dat' },
	{ label: 'Loyalsoldier (jsdelivr)', value: 'https://cdn.jsdelivr.net/gh/Loyalsoldier/geoip@release/geoip.dat' },
	{ label: 'v2fly (GitHub)', value: 'https://github.com/v2fly/geoip/releases/latest/download/geoip.dat' },
	{ label: 'Loyalsoldier (fastly)', value: 'https://fastly.jsdelivr.net/gh/Loyalsoldier/geoip@release/geoip.dat' }
];

var adFilterPresets = [
	{ label: 'AdGuard SDNS Filter', value: 'https://adguardteam.github.io/AdGuardSDNSFilter/Filters/filter.txt' },
	{ label: 'AdGuard SDNS (jsdelivr)', value: 'https://cdn.jsdelivr.net/gh/AdguardTeam/AdguardSDNSFilter@master/Filters/filter.txt' },
	{ label: 'EasyList', value: 'https://easylist-downloads.adblockplus.org/easylist.txt' },
	{ label: 'Anti-AD', value: 'https://anti-ad.net/easylist.txt' }
];

function createUrlInputWithPresets(inputId, presets, currentValue) {
	var wrapper = E('div', { 'style': 'width:100%;display:flex;gap:4px;align-items:center;flex-direction:column' });

	var input = E('input', {
		'type': 'text', 'class': 'cbi-input-text',
		'value': currentValue || '',
		'id': inputId,
		'style': 'width:100%'
	});
	wrapper.appendChild(input);

	var select = E('select', {
		'class': 'cbi-input-select',
		'style': 'width:100%'
	});
	var defaultOpt = E('option', { 'value': '' }, '预设源');
	select.appendChild(defaultOpt);
	presets.forEach(function(p) {
		var opt = E('option', { 'value': p.value }, p.label);
		select.appendChild(opt);
	});
	select.addEventListener('change', function() {
		if (this.value) {
			input.value = this.value;
			this.selectedIndex = 0;
		}
	});
	wrapper.appendChild(select);

	return wrapper;
}

function createBackupUrlEditor(inputId, currentValues) {
	var wrapper = E('div', { 'style': 'width:100%' });
	var urls = (currentValues && currentValues.length > 0) ? currentValues.slice() : [];

	var listDiv = E('div', { 'id': inputId + '-list', 'style': 'margin-bottom:4px' });
	urls.forEach(function(url, idx) {
		listDiv.appendChild(createBackupUrlRow(inputId, url, idx, urls, listDiv));
	});
	wrapper.appendChild(listDiv);

	var addBtn = E('button', {
		'class': 'cbi-button',
		'type': 'button',
		'style': 'font-size:11px;padding:2px 8px'
	}, '+ 添加备用源');
	addBtn.addEventListener('click', function() {
		urls.push('');
		var idx = urls.length - 1;
		listDiv.appendChild(createBackupUrlRow(inputId, '', idx, urls, listDiv));
	});
	wrapper.appendChild(addBtn);
	wrapper.appendChild(E('input', { 'type': 'hidden', 'id': inputId, 'value': '' }));

	wrapper._getUrls = function() {
		var result = [];
		var inputs = listDiv.querySelectorAll('input[type="text"]');
		inputs.forEach(function(inp) {
			var v = (inp.value || '').trim();
			if (v) result.push(v);
		});
		return result;
	};

	return wrapper;
}

function createBackupUrlRow(baseId, url, idx, urls, listDiv) {
	var row = E('div', { 'style': 'margin-bottom:2px;display:flex;align-items:center;gap:4px' });
	var input = E('input', {
		'type': 'text', 'class': 'cbi-input-text',
		'value': url || '',
		'placeholder': 'https://备用下载地址...',
		'style': 'flex:1'
	});
	row.appendChild(input);

	var removeBtn = E('button', {
		'class': 'cbi-button cbi-button-remove',
		'type': 'button',
		'style': 'min-width:30px'
	}, 'x');
	removeBtn.addEventListener('click', function() {
		var val = input.value.trim();
		var i = urls.indexOf(val);
		if (i === -1) {
			for (var j = 0; j < urls.length; j++) {
				if (listDiv.children[j] === row) { i = j; break; }
			}
		}
		if (i > -1) urls.splice(i, 1);
		listDiv.removeChild(row);
	});
	row.appendChild(removeBtn);

	return row;
}

function refreshProgress() {
	apiGet('/geo_progress').then(function(resp) {
		var content = document.getElementById('progress-content');
		if (!content) {
			stopProgressTimer();
			return;
		}

		if (resp.code !== 0 || !resp.data) {
			content.style.display = 'none';
			stopProgressTimer();
			return;
		}

		var data = resp.data;
		var targets = ['geosite', 'geoip', 'ad_filter'];
		var activeCount = 0;
		var html = '';

		targets.forEach(function(target) {
			var p = data[target];
			if (!p) return;

			var statusIcon = '';
			var statusText = '';
			switch (p.status) {
				case 'downloading':
					statusIcon = '[下载]';
					statusText = '下载中';
					activeCount++;
					break;
				case 'loading':
					statusIcon = '[加载]';
					statusText = '加载中';
					activeCount++;
					break;
				case 'completed':
					statusIcon = '[完成]';
					statusText = '已完成';
					break;
				case 'failed':
					statusIcon = '[失败]';
					statusText = '失败';
					break;
				default:
					return;
			}

			var targetName = { geosite: 'GeoSite', geoip: 'GeoIP', ad_filter: '广告过滤' }[target] || target;
			html += '<div style="margin-bottom:8px;padding:6px;border:1px solid #ddd;border-radius:4px">';

			if (p.status === 'downloading') {
				var percent = (p.percent >= 0) ? p.percent : 0;
				var progressBarColor = '#4CAF50';
				html += '<div style="font-weight:bold">' + statusIcon + ' ' + targetName + ' - ' + statusText;
				if (p.source_total > 1) {
					html += ' (源 ' + p.source_index + '/' + p.source_total + ')';
				}
				html += '</div>';
				html += '<div style="background:#eee;border-radius:3px;height:12px;margin:4px 0;overflow:hidden">';
				html += '<div style="background:' + progressBarColor + ';height:100%;width:' + percent + '%;transition:width 0.3s"></div>';
				html += '</div>';
				html += '<div style="font-size:11px;color:#666">';
				html += formatBytes(p.downloaded_bytes || 0);
				if (p.total_bytes > 0) html += ' / ' + formatBytes(p.total_bytes);
				html += ' | 速度: ' + formatSpeed(p.speed_bps);
				html += ' | ' + percent + '%';
				html += '</div>';
				html += '<div style="font-size:10px;color:#999;word-break:break-all">源: ' + (p.url || '') + '</div>';
			} else {
				html += '<div style="font-weight:bold">' + statusIcon + ' ' + targetName + ' - ' + statusText + '</div>';
				if (p.message) {
					html += '<div style="font-size:11px;color:#666">' + p.message + '</div>';
				}
				if (p.url) {
					html += '<div style="font-size:10px;color:#999;word-break:break-all">源: ' + p.url + '</div>';
				}
			}

			html += '</div>';
		});

		if (!html) {
			content.style.display = 'none';
			stopProgressTimer();
		} else {
			content.innerHTML = html;
			content.style.display = '';
			if (activeCount === 0) {
				setTimeout(function() {
					content.style.display = 'none';
				}, 3000);
				stopProgressTimer();
			}
		}
	}).catch(function() {
		stopProgressTimer();
	});
}

function startProgressTimer() {
	stopProgressTimer();
	progressTimer = setInterval(refreshProgress, 1000);
	refreshProgress();
}

function stopProgressTimer() {
	if (progressTimer) {
		clearInterval(progressTimer);
		progressTimer = null;
	}
}

return view.extend({
	load: function() {
		apiBase = L.url('admin/services/lynxdns/api');
		return Promise.all([
			apiGet('/config').catch(function() { return { code: 500 }; }),
			apiGet('/geo_status').catch(function() { return { code: 500 }; })
		]);
	},

	render: function(data) {
		if (!document.querySelector('link[href*="lynxdns.css"]')) {
			document.head.appendChild(E('link', { 'rel': 'stylesheet', 'type': 'text/css', 'href': '/luci-static/lynxdns.css' }));
		}
		var configResp = data[0] || {};
		var geoResp = data[1] || {};
		var config = (configResp.code === 0) ? configResp.data : {};
		var geoStatus = (geoResp.code === 0) ? geoResp.data : null;
		var geo = config.geo || {};

		var m = E('div', { 'class': 'cbi-map' }, [
			E('div', { 'class': 'cbi-map-descr' }, '管理 GeoSite 域名库、GeoIP IP 库和广告过滤规则库的下载与更新。支持多源故障转移。')
		]);

		var statusSection = E('fieldset', { 'class': 'cbi-section' }, [
			E('legend', {}, '数据库状态')
		]);
		if (geoStatus) {
			var gs = geoStatus.geosite || {};
			var gi = geoStatus.geoip || {};
			var ad = geoStatus.ad_filter || {};

			var gsCard = E('div', { 'class': 'geo-status-card' }, [
				E('div', { 'class': 'geo-status-header' }, [
					E('span', { 'class': 'geo-status-title' }, 'GeoSite 域名库'),
					E('span', { 'class': 'geo-status-badge ' + (gs.loaded ? 'loaded' : 'unloaded') },
						gs.loaded ? '已加载' : '未加载')
				]),
				gs.loaded ? E('div', { 'class': 'geo-status-grid' }, [
					E('div', {}, [E('span', { 'class': 'geo-status-label' }, '版本：'), E('span', { 'class': 'geo-status-value' }, gs.version || 'N/A')]),
					E('div', {}, [E('span', { 'class': 'geo-status-label' }, '更新：'), E('span', { 'class': 'geo-status-value' }, formatDate(gs.last_update))]),
					E('div', {}, [E('span', { 'class': 'geo-status-label' }, '大小：'), E('span', { 'class': 'geo-status-value' }, formatBytes(gs.size_bytes || 0))]),
					E('div', {}, [E('span', { 'class': 'geo-status-label' }, '分类：'), E('span', { 'class': 'geo-status-value' }, (gs.categories_count || 0) + ' 个')])
				]) : E('div', { 'class': 'geo-status-empty' }, '暂无数据')
			]);
			statusSection.appendChild(gsCard);

			var giCard = E('div', { 'class': 'geo-status-card' }, [
				E('div', { 'class': 'geo-status-header' }, [
					E('span', { 'class': 'geo-status-title' }, 'GeoIP 地址库'),
					E('span', { 'class': 'geo-status-badge ' + (gi.loaded ? 'loaded' : 'unloaded') },
						gi.loaded ? '已加载' : '未加载')
				]),
				gi.loaded ? E('div', { 'class': 'geo-status-grid' }, [
					E('div', {}, [E('span', { 'class': 'geo-status-label' }, '版本：'), E('span', { 'class': 'geo-status-value' }, gi.version || 'N/A')]),
					E('div', {}, [E('span', { 'class': 'geo-status-label' }, '更新：'), E('span', { 'class': 'geo-status-value' }, formatDate(gi.last_update))]),
					E('div', {}, [E('span', { 'class': 'geo-status-label' }, '大小：'), E('span', { 'class': 'geo-status-value' }, formatBytes(gi.size_bytes || 0))]),
					E('div', {}, [E('span', { 'class': 'geo-status-label' }, '条目：'), E('span', { 'class': 'geo-status-value' }, (gi.entries_count || 0) + ' 条')])
				]) : E('div', { 'class': 'geo-status-empty' }, '暂无数据')
			]);
			statusSection.appendChild(giCard);

			var adCard = E('div', { 'class': 'geo-status-card' }, [
				E('div', { 'class': 'geo-status-header' }, [
					E('span', { 'class': 'geo-status-title' }, '广告过滤规则'),
					E('span', { 'class': 'geo-status-badge ' + (ad.loaded ? 'loaded' : 'unloaded') },
						ad.loaded ? '已加载' : '未加载')
				]),
				ad.loaded ? E('div', { 'class': 'geo-status-grid' }, [
					E('div', {}, [E('span', { 'class': 'geo-status-label' }, '规则：'), E('span', { 'class': 'geo-status-value' }, (ad.rule_count || 0) + ' 条')]),
					E('div', {}, [E('span', { 'class': 'geo-status-label' }, '更新：'), E('span', { 'class': 'geo-status-value' }, formatDate(ad.last_update))])
				]) : E('div', { 'class': 'geo-status-empty' }, '暂无数据')
			]);
			statusSection.appendChild(adCard);

		} else {
			statusSection.appendChild(fieldRow('状态', '无法连接到 LynxDNS 内核'));
		}
		m.appendChild(statusSection);

		var updateSection = E('fieldset', { 'class': 'cbi-section' }, [
			E('legend', {}, '自动更新设置')
		]);
		var autoUpdateSwitch = E('input', { 'type': 'checkbox', 'id': 'cb-autoupdate' });
		if (geo.auto_update !== false) autoUpdateSwitch.checked = true;
		updateSection.appendChild(fieldRow('自动更新', autoUpdateSwitch));

		cronInput = E('input', {
			'type': 'hidden',
			'value': geo.update_cron || '0 3 * * *',
			'id': 'cron-input'
		});
		updateSection.appendChild(cronInput);

		var cronDaySelect = E('select', { 'class': 'cbi-input-select', 'style': 'width:100%' });
		var dayOptions = [
			{ label: '每天', value: '*' },
			{ label: '周一', value: '1' },
			{ label: '周二', value: '2' },
			{ label: '周三', value: '3' },
			{ label: '周四', value: '4' },
			{ label: '周五', value: '5' },
			{ label: '周六', value: '6' },
			{ label: '周日', value: '0' },
			{ label: '周一至周五', value: '1-5' },
			{ label: '周六和周日', value: '0,6' }
		];
		dayOptions.forEach(function(opt) {
			var o = E('option', { 'value': opt.value }, opt.label);
			cronDaySelect.appendChild(o);
		});

		var cronHourSelect = E('select', { 'class': 'cbi-input-select', 'style': 'width:100%' });
		for (var h = 0; h < 24; h++) {
			var o = E('option', { 'value': String(h) }, h + ':00');
			cronHourSelect.appendChild(o);
		}

		function parseCronToSelects(cron) {
			var parts = (cron || '0 3 * * *').split(/\s+/);
			var hour = parseInt(parts[1]);
			if (isNaN(hour) || hour < 0 || hour > 23) hour = 3;
			var dow = parts[4] || '*';
			cronHourSelect.value = String(hour);
			cronDaySelect.value = dow;
		}

		function updateCronFromSelects() {
			var hour = cronHourSelect.value;
			var dow = cronDaySelect.value;
			document.getElementById('cron-input').value = '0 ' + hour + ' * * ' + dow;
		}

		parseCronToSelects(geo.update_cron);

		cronDaySelect.addEventListener('change', updateCronFromSelects);
		cronHourSelect.addEventListener('change', updateCronFromSelects);

		updateSection.appendChild(E('div', { 'class': 'cbi-value' }, [
			E('label', { 'class': 'cbi-value-title' }, '更新规则'),
			E('div', { 'class': 'cbi-value-field' }, [cronDaySelect])
		]));
		updateSection.appendChild(E('div', { 'class': 'cbi-value' }, [
			E('label', { 'class': 'cbi-value-title' }, '时间'),
			E('div', { 'class': 'cbi-value-field' }, [cronHourSelect])
		]));

		dataDirInput = E('input', {
			'type': 'text', 'class': 'cbi-input-text',
			'value': geo.data_dir || '/etc/lynxdns/data'
		});
		updateSection.appendChild(fieldRow('数据目录', dataDirInput));
		m.appendChild(updateSection);

		var sourceSection = E('fieldset', { 'class': 'cbi-section' }, [
			E('legend', {}, '数据源地址（含备用源故障转移）')
		]);

		sourceSection.appendChild(E('div', { 'class': 'cbi-value-description' },
			'设置主下载地址和备用地址。当主地址下载失败时，将自动依次尝试备用地址。'));

		var geositeUrlInput = createUrlInputWithPresets(
			'geosite-url',
			geositePresets,
			geo.geosite_url || 'https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat'
		);
		sourceSection.appendChild(fieldRow('GeoSite 主地址', geositeUrlInput));

		geositeBackupEditor = createBackupUrlEditor('geosite-backup', geo.geosite_backup_url || []);
		sourceSection.appendChild(fieldRow('GeoSite 备用地址', geositeBackupEditor));

		var geoipUrlInput = createUrlInputWithPresets(
			'geoip-url',
			geoipPresets,
			geo.geoip_url || 'https://github.com/Loyalsoldier/geoip/releases/latest/download/geoip.dat'
		);
		sourceSection.appendChild(fieldRow('GeoIP 主地址', geoipUrlInput));

		geoipBackupEditor = createBackupUrlEditor('geoip-backup', geo.geoip_backup_url || []);
		sourceSection.appendChild(fieldRow('GeoIP 备用地址', geoipBackupEditor));

		var adFilterUrlInput = createUrlInputWithPresets(
			'adfilter-url',
			adFilterPresets,
			geo.ad_filter_url || 'https://adguardteam.github.io/AdGuardSDNSFilter/Filters/filter.txt'
		);
		sourceSection.appendChild(fieldRow('广告过滤主地址', adFilterUrlInput));

		adFilterBackupEditor = createBackupUrlEditor('adfilter-backup', geo.ad_filter_backup_url || []);
		sourceSection.appendChild(fieldRow('广告过滤备用地址', adFilterBackupEditor));

		m.appendChild(sourceSection);

		var manualSection = E('fieldset', { 'class': 'cbi-section' }, [
			E('legend', {}, '手动更新')
		]);

		var updateAllBtn = E('button', {
			'class': 'cbi-button cbi-button-apply',
			'type': 'button'
		}, '全部更新');
		updateAllBtn.addEventListener('click', function() {
			showToast('正在开始更新所有数据库...');
			apiPost('/geo_update', { target: 'all' }).then(function() {
				startProgressTimer();
			});
		});
		manualSection.appendChild(fieldRow('全部更新', updateAllBtn));

		var updateGeositeBtn = E('button', {
			'class': 'cbi-button cbi-button-apply',
			'type': 'button'
		}, '更新 GeoSite');
		updateGeositeBtn.addEventListener('click', function() {
			showToast('GeoSite 更新已启动。');
			apiPost('/geo_update', { target: 'geosite' }).then(function() {
				startProgressTimer();
			});
		});
		manualSection.appendChild(fieldRow('更新 GeoSite', updateGeositeBtn));

		var updateGeoipBtn = E('button', {
			'class': 'cbi-button cbi-button-apply',
			'type': 'button'
		}, '更新 GeoIP');
		updateGeoipBtn.addEventListener('click', function() {
			showToast('GeoIP 更新已启动。');
			apiPost('/geo_update', { target: 'geoip' }).then(function() {
				startProgressTimer();
			});
		});
		manualSection.appendChild(fieldRow('更新 GeoIP', updateGeoipBtn));

		var updateAdBtn = E('button', {
			'class': 'cbi-button cbi-button-apply',
			'type': 'button'
		}, '更新广告过滤');
		updateAdBtn.addEventListener('click', function() {
			showToast('广告过滤规则更新已启动。');
			apiPost('/geo_update', { target: 'ad_filter' }).then(function() {
				startProgressTimer();
			});
		});
		manualSection.appendChild(fieldRow('更新广告过滤', updateAdBtn));

		var progressContent = E('div', {
			'id': 'progress-content',
			'style': 'margin-top:12px;padding:8px;background:#f5f5f5;border-radius:4px;display:none'
		});
		manualSection.appendChild(progressContent);
		m.appendChild(manualSection);

		var historySection = E('fieldset', { 'class': 'cbi-section' }, [
			E('legend', {}, '更新历史记录')
		]);
		var historyContent = E('div', { 'id': 'history-content' });
		historyContent.innerHTML = '<em>加载中...</em>';
		historySection.appendChild(historyContent);

		var refreshHistoryBtn = E('button', {
			'class': 'cbi-button',
			'type': 'button',
			'style': 'font-size:11px;padding:2px 8px;margin-top:4px'
		}, '刷新历史');
		refreshHistoryBtn.addEventListener('click', function() {
			loadHistory();
		});
		historySection.appendChild(refreshHistoryBtn);
		m.appendChild(historySection);

		function loadHistory() {
			apiGet('/geo_history?limit=20').then(function(resp) {
				var content = document.getElementById('history-content');
				if (!content) return;

				if (resp.code !== 0 || !resp.data || !resp.data.history || resp.data.history.length === 0) {
					content.innerHTML = '<em>暂无更新记录</em>';
					return;
				}

				var html = '<table style="width:100%;border-collapse:collapse;font-size:12px">';
				html += '<tr style="border-bottom:2px solid #ddd;font-weight:bold">';
				html += '<th style="text-align:left;padding:8px 6px">时间</th>';
				html += '<th style="text-align:left;padding:8px 6px">目标</th>';
				html += '<th style="text-align:left;padding:8px 6px">状态</th>';
				html += '<th style="text-align:left;padding:8px 6px">源地址</th>';
				html += '<th style="text-align:left;padding:8px 6px">耗时</th>';
				html += '<th style="text-align:left;padding:8px 6px">说明</th>';
				html += '</tr>';

				resp.data.history.reverse().forEach(function(entry) {
					var statusIcon = '';
					switch (entry.status) {
						case 'success': statusIcon = '[成功]'; break;
						case 'failed': statusIcon = '[失败]'; break;
						default: statusIcon = '[未知]';
					}

					var targetName = { geosite: 'GeoSite', geoip: 'GeoIP', ad_filter: '广告过滤' }[entry.target] || entry.target;

					html += '<tr style="border-bottom:1px solid #eee">';
					html += '<td style="padding:8px 6px;white-space:nowrap">' + formatDate(entry.timestamp) + '</td>';
					html += '<td style="padding:8px 6px">' + targetName + '</td>';
					html += '<td style="padding:8px 6px">' + statusIcon + ' ' + entry.status + '</td>';
					html += '<td style="padding:8px 6px;max-width:200px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="' + (entry.url || '') + '">' + (entry.url || '-') + '</td>';
					html += '<td style="padding:8px 6px">' + formatDuration(entry.duration_ms) + '</td>';
					html += '<td style="padding:8px 6px">' + (entry.message || '') + '</td>';
					html += '</tr>';
				});

				html += '</table>';
				content.innerHTML = html;
			}).catch(function() {
				var content = document.getElementById('history-content');
				if (content) content.innerHTML = '<em>加载失败</em>';
			});
		}

		loadHistory();

		return m;
	},

	handleSaveApply: function(ev) {
		var geositeUrlEl = document.getElementById('geosite-url');
		var geoipUrlEl = document.getElementById('geoip-url');
		var adfilterUrlEl = document.getElementById('adfilter-url');

		var patchData = {
			geo: {
				auto_update: document.getElementById('cb-autoupdate').checked,
				update_cron: cronInput.value,
				data_dir: dataDirInput.value,
				geosite_url: geositeUrlEl ? geositeUrlEl.value : '',
				geosite_backup_url: geositeBackupEditor._getUrls(),
				geoip_url: geoipUrlEl ? geoipUrlEl.value : '',
				geoip_backup_url: geoipBackupEditor._getUrls(),
				ad_filter_url: adfilterUrlEl ? adfilterUrlEl.value : '',
				ad_filter_backup_url: adFilterBackupEditor._getUrls()
			}
		};
		apiPatch('/config', patchData).then(function(resp) {
			if (resp.code === 0) {
				showToast('Geo 数据设置已保存并热加载完成。');
			} else {
				showToast('保存失败：' + (resp.message || '未知错误'));
			}
		});
	},

	handleSave: function(ev) {
		return this.handleSaveApply(ev);
	}
});
