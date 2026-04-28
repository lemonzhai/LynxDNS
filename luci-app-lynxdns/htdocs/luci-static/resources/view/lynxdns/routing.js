'use strict';
'require view';
'require ui';

var apiBase;
var geositeRemoteList, geositeDomesticList, geositeAdFilterList, geoipRemoteList;

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

function showToast(msg) {
	var existing = document.querySelectorAll('.lynxdns-toast');
	existing.forEach(function(el) { el.remove(); });
	var t = E('div', { 'class': 'lynxdns-toast' }, msg);
	document.body.appendChild(t);
	setTimeout(function() { if (t.parentNode) t.remove(); }, 2500);
}

function fieldRow(label, content) {
	return E('div', { 'class': 'cbi-value' }, [
		E('label', { 'class': 'cbi-value-title' }, label),
		E('div', { 'class': 'cbi-value-field' }, (typeof content === 'string') ? [E('span', {}, content)] : [content])
	]);
}

function createTagList(items, suggestions, placeholder) {
	var container = E('div', { 'class': 'routing-list-container', 'style': 'margin-bottom:5px' });
	var list = items || [];

	function renderItem(tag) {
		var row = E('div', { 'style': 'display:flex;margin-bottom:4px;gap:4px;align-items:center' }, [
			E('input', {
				'type': 'text', 'class': 'cbi-input-text',
				'value': tag, 'placeholder': placeholder
			}),
			E('button', {
				'class': 'cbi-button cbi-button-remove',
				'type': 'button', 'style': 'min-width:30px',
				'click': function() { row.parentNode.removeChild(row); }
			}, 'x')
		]);
		container.appendChild(row);
	}

	list.forEach(renderItem);

	var addBtn = E('button', {
		'class': 'cbi-button cbi-button-add',
		'type': 'button',
		'click': function() { renderItem(''); }
	}, '添加');
	container.appendChild(addBtn);

	if (suggestions && suggestions.length > 0) {
		var suggestDiv = E('div', { 'style': 'display:flex;flex-wrap:wrap;gap:4px;margin-top:6px' });
		suggestions.forEach(function(s) {
			var btn = E('button', {
				'class': 'cbi-button',
				'type': 'button',
				'style': 'font-size:11px;padding:2px 8px'
			}, s);
			btn.addEventListener('click', function() {
				var inputs = container.querySelectorAll('input[type="text"]');
				var exists = false;
				inputs.forEach(function(inp) {
					if (inp.value.trim() === s) exists = true;
				});
				if (!exists) renderItem(s);
			});
			suggestDiv.appendChild(btn);
		});
		container.appendChild(suggestDiv);
	}

	container.getValues = function() {
		var inputs = container.querySelectorAll('input[type="text"]');
		var vals = [];
		inputs.forEach(function(inp) {
			if (inp.value.trim()) vals.push(inp.value.trim());
		});
		return vals;
	};

	return container;
}

return view.extend({
	load: function() {
		apiBase = L.url('admin/services/lynxdns/api');
		return apiGet('/config').catch(function() { return { code: 500 }; });
	},

	render: function(data) {
		if (!document.querySelector('link[href*="lynxdns.css"]')) {
			document.head.appendChild(E('link', { 'rel': 'stylesheet', 'type': 'text/css', 'href': '/luci-static/lynxdns.css' }));
		}
		var config = (data && data.code === 0) ? data.data : {};
		var routing = config.routing || {};
		var geosite = routing.geosite || {};
		var geoip = routing.geoip || {};

		var m = E('div', { 'class': 'cbi-map' }, [
			E('div', { 'class': 'cbi-map-descr' }, '配置基于 GeoSite 和 GeoIP 的 DNS 分流规则，决定域名和 IP 使用哪组 DNS 服务器解析。')
		]);

		var infoBox = E('div', { 'class': 'priority-info', 'style': 'margin-top:20px' }, [
			E('strong', {}, '分流规则说明：'),
			E('div', { 'style': 'margin-top:4px' },
				'当 DNS 查询到达时，LynxDNS 按以下优先级依次检查规则：' +
				'自定义规则 > 广告过滤 > GeoSite 分类 > GeoIP 分类 > 默认策略。' +
				'下方配置的 GeoSite 分类决定匹配到的域名使用哪组 DNS 服务器。')
		]);
		m.appendChild(infoBox);

		geositeRemoteList = createTagList(
			geosite.remote || ['geolocation-!cn', 'google', 'github', 'gfw'],
			['geolocation-!cn', 'google', 'github', 'gfw', 'telegram', 'twitter', 'facebook', 'netflix', 'spotify', 'microsoft', 'apple', 'tld-cn'],
			'例如 geolocation-!cn'
		);
		var geositeRemoteSection = E('fieldset', { 'class': 'cbi-section' }, [
			E('legend', {}, 'GeoSite - 远程 DNS'),
			E('div', { 'class': 'cbi-section-descr' }, '匹配这些 GeoSite 分类的域名将使用远程 DNS 服务器解析。通常包含海外域名，需要通过远程 DNS 获取正确结果。')
		]);
		geositeRemoteSection.appendChild(E('div', { 'class': 'cbi-value' }, [
			E('label', { 'class': 'cbi-value-title' }, '分类列表'),
			E('div', { 'class': 'cbi-value-field' }, [geositeRemoteList])
		]));
		m.appendChild(geositeRemoteSection);

		geositeDomesticList = createTagList(
			geosite.domestic || ['cn'],
			['cn', 'cn-apps', 'tld-cn', 'bilibili', 'taobao', 'baidu', 'alibaba', 'tencent', '163'],
			'例如 cn'
		);
		var geositeDomesticSection = E('fieldset', { 'class': 'cbi-section' }, [
			E('legend', {}, 'GeoSite - 国内 DNS'),
			E('div', { 'class': 'cbi-section-descr' }, '匹配这些 GeoSite 分类的域名将使用国内 DNS 服务器解析。通常包含中国大陆域名。')
		]);
		geositeDomesticSection.appendChild(E('div', { 'class': 'cbi-value' }, [
			E('label', { 'class': 'cbi-value-title' }, '分类列表'),
			E('div', { 'class': 'cbi-value-field' }, [geositeDomesticList])
		]));
		m.appendChild(geositeDomesticSection);

		geositeAdFilterList = createTagList(
			geosite.ad_filter || ['category-ads-all'],
			['category-ads-all', 'category-ads', 'gfw', 'adguard-dns-filter'],
			'例如 category-ads-all'
		);
		var geositeAdFilterSection = E('fieldset', { 'class': 'cbi-section' }, [
			E('legend', {}, 'GeoSite - 广告拦截'),
			E('div', { 'class': 'cbi-section-descr' }, '匹配这些 GeoSite 分类的域名将被拦截（返回 NXDOMAIN）。用于广告和追踪器过滤。')
		]);
		geositeAdFilterSection.appendChild(E('div', { 'class': 'cbi-value' }, [
			E('label', { 'class': 'cbi-value-title' }, '分类列表'),
			E('div', { 'class': 'cbi-value-field' }, [geositeAdFilterList])
		]));
		m.appendChild(geositeAdFilterSection);

		geoipRemoteList = createTagList(
			geoip.remote || ['!cn'],
			['!cn', 'us', 'jp', 'kr', 'de', 'gb', 'sg', 'hk', 'tw'],
			'例如 !cn'
		);
		var geoipRemoteSection = E('fieldset', { 'class': 'cbi-section' }, [
			E('legend', {}, 'GeoIP - 远程 DNS'),
			E('div', { 'class': 'cbi-section-descr' }, 'DNS 解析结果 IP 匹配这些 GeoIP 分类的查询将触发远程 DNS 重新解析。这是 GeoSite 规则未匹配时的回退策略。')
		]);
		geoipRemoteSection.appendChild(E('div', { 'class': 'cbi-value' }, [
			E('label', { 'class': 'cbi-value-title' }, '分类列表'),
			E('div', { 'class': 'cbi-value-field' }, [geoipRemoteList])
		]));
		m.appendChild(geoipRemoteSection);

		return m;
	},

	handleSaveApply: function(ev) {
		var patchData = {
			routing: {
				geosite: {
					remote: geositeRemoteList.getValues(),
					domestic: geositeDomesticList.getValues(),
					ad_filter: geositeAdFilterList.getValues()
				},
				geoip: {
					remote: geoipRemoteList.getValues()
				}
			}
		};
		apiPatch('/config', patchData).then(function(resp) {
			if (resp.code === 0) {
				showToast('分流规则已保存并热加载完成。');
			} else {
				showToast('保存失败：' + (resp.message || '未知错误'));
			}
		});
	},

	handleSave: function(ev) {
		return this.handleSaveApply(ev);
	}
});
