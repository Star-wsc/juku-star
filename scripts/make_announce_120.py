# -*- coding: utf-8 -*-
from PIL import Image, ImageDraw, ImageFont
import os

W, H = 1080, 1980
bg = (11, 18, 32)
card = (22, 32, 51)
ink = (232, 238, 252)
ink2 = (168, 182, 207)
ink3 = (123, 138, 166)
accent = (91, 140, 255)
ok = (34, 197, 94)

img = Image.new("RGB", (W, H), bg)
draw = ImageDraw.Draw(img, "RGBA")


def font(size, bold=False):
    path = r"C:\Windows\Fonts\msyhbd.ttc" if bold else r"C:\Windows\Fonts\msyh.ttc"
    return ImageFont.truetype(path, size)


def rrect(xy, r, fill):
    draw.rounded_rectangle(xy, radius=r, fill=fill)


# 顶部渐变条
for i in range(8):
    t = i / 7
    col = (int(91 + (124 - 91) * t), int(140 + (92 - 140) * t), 255)
    draw.rectangle((0, i * 3, W, (i + 1) * 3), fill=col)

draw.text((72, 88), "剧库 1.2.0", font=font(64, True), fill=ink)
draw.text((72, 168), "更新公告", font=font(40, True), fill=accent)
draw.rounded_rectangle((72, 230, 250, 280), radius=22, fill=(91, 140, 255, 36))
draw.text((92, 240), "2026-10-07", font=font(22), fill=(184, 204, 255))

y = 320
for name, note, col in [
    ("剧库完整版", "全站源 · 推荐安装", accent),
    ("剧库红果版", "仅红果 · 独立安装", ok),
]:
    rrect((72, y, W - 72, y + 88), 18, card)
    draw.ellipse((96, y + 28, 124, y + 56), fill=col)
    draw.text((148, y + 22), name, font=font(28, True), fill=ink)
    draw.text((148, y + 52), note, font=font(20), fill=ink3)
    y += 104


def section(title, items, y, tag, tag_col):
    draw.rounded_rectangle((72, y, 72 + len(tag) * 14 + 28, y + 36), radius=12, fill=(*tag_col, 40))
    draw.text((88, y + 6), tag, font=font(18, True), fill=tag_col)
    y += 48
    draw.text((72, y), title, font=font(32, True), fill=ink)
    y += 52
    for icon, head, body in items:
        rrect((72, y, W - 72, y + 100), 16, card)
        draw.rounded_rectangle((92, y + 22, 132, y + 62), radius=10, fill=(*accent, 50))
        draw.text((102, y + 26), icon, font=font(22, True), fill=ink)
        draw.text((152, y + 18), head, font=font(24, True), fill=ink)
        draw.text((152, y + 52), body, font=font(18), fill=ink2)
        y += 112
    return y + 12


y = section(
    "新增能力",
    [
        ("窗", "小窗播放", "按 Home 或点「小窗」画中画悬浮，后台继续看"),
        ("果", "剧库红果版", "只看红果，与账号无关，可与完整版并存"),
        ("搜", "搜索联想", "2.0 服务器输入即出建议词（旧版静默降级）"),
        ("彩", "彩色封面占位", "iOS 风渐变色块 + 剧名，加载失败不露灰块"),
        ("板", "平板 / 横屏", "封面 3-8 列自适应，不再被拉爆"),
    ],
    y,
    "NEW",
    accent,
)

y = section(
    "体验优化",
    [
        ("势", "播放手势三分区", "左退10s · 中播放/暂停 · 右进10s；双击30s"),
        ("声", "音量 / 亮度修复", "左右竖滑调节跟手，不再抖动或调不动"),
        ("容", "2.0 服务端兼容", "取流失败自动重试；解码提示；新旧服通用"),
    ],
    y,
    "OPT",
    ok,
)

footer_top = max(y + 16, H - 150)
rrect((72, footer_top, W - 72, footer_top + 88), 16, (27, 39, 64))
draw.text((96, footer_top + 18), "安装说明", font=font(20, True), fill=ink)
draw.text((96, footer_top + 48), "日常请装 Release 正式包 · 版本号随每次更新递增", font=font(18), fill=ink2)

out = r"C:\Users\wsc76\Desktop\短剧库Android开发方案\剧库1.2.0更新公告.png"
img.save(out, "PNG")
print("saved", out, os.path.getsize(out), "footer_top", footer_top, "y", y)
