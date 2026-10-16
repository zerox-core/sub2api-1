<template>
  <div class="pointer-events-none absolute inset-0 overflow-hidden select-none">
    <!-- 纯黑曜石深空底层 -->
    <div class="absolute inset-0 bg-[#07080C]"></div>

    <!-- 琥珀极弱暗晕（仅在极弱暗部提供空间微弱纵深，绝对杜绝任何蓝紫色） -->
    <div class="absolute left-1/2 top-0 -translate-x-1/2 h-[600px] w-[900px] rounded-full bg-amber-500/[0.035] blur-[170px]"></div>
    <div class="absolute -right-32 top-1/3 h-[450px] w-[450px] rounded-full bg-orange-500/[0.025] blur-[150px]"></div>
    <div class="absolute -left-32 bottom-1/4 h-[400px] w-[400px] rounded-full bg-amber-600/[0.02] blur-[150px]"></div>

    <!-- 微星点层 1：远景细密微尘星点 (1px 纯白，慢速微闪) -->
    <div class="stars-layer stars-small"></div>

    <!-- 微星点层 2：中景清晰白色星光 (1.5px 纯白，交错呼吸) -->
    <div class="stars-layer stars-medium"></div>

    <!-- 微星点层 3：近景明亮微光星核 (2.2px 纯白，带微弱扩散光晕) -->
    <div class="stars-layer stars-large"></div>

    <!-- 深空微十字星芒恒星点（Cosmic Lens Starpoints - 纯白十字锐光） -->
    <div class="star-flare star-flare-1"></div>
    <div class="star-flare star-flare-2"></div>
    <div class="star-flare star-flare-3"></div>
    <div class="star-flare star-flare-4"></div>
    <div class="star-flare star-flare-5"></div>
    <div class="star-flare star-flare-6"></div>

    <!-- 纯白深空微流星轨迹（Shooting Star 优雅划过） -->
    <div class="shooting-star shooting-star-1"></div>
    <div class="shooting-star shooting-star-2"></div>

    <!-- 极弱极客微网格纹理（与星空融合，赋予 AI 科技基础设施秩序感） -->
    <div class="absolute inset-0 bg-[linear-gradient(rgba(255,255,255,0.015)_1px,transparent_1px),linear-gradient(90deg,rgba(255,255,255,0.015)_1px,transparent_1px)] bg-[size:60px_60px] [mask-image:radial-gradient(ellipse_at_center,black_40%,transparent_80%)]"></div>
  </div>
</template>

<style scoped>
/* 星空层基础配置 */
.stars-layer {
  position: absolute;
  inset: 0;
  pointer-events: none;
}

/* 远景细密微星点：纯白色微尘，遍布整个屏幕，轻柔慢闪 */
.stars-small {
  width: 1px;
  height: 1px;
  background: transparent;
  box-shadow:
    30px 60px #ffffff, 90px 180px rgba(255, 255, 255, 0.7), 210px 110px rgba(255, 255, 255, 0.9),
    340px 320px #ffffff, 480px 160px rgba(255, 255, 255, 0.6), 620px 80px #ffffff,
    710px 290px rgba(255, 255, 255, 0.8), 840px 140px #ffffff, 950px 360px rgba(255, 255, 255, 0.6),
    1080px 70px #ffffff, 1210px 240px rgba(255, 255, 255, 0.8), 1330px 150px #ffffff,
    1450px 420px rgba(255, 255, 255, 0.7), 1600px 210px #ffffff, 1720px 110px rgba(255, 255, 255, 0.9),
    1850px 340px #ffffff, 120px 580px rgba(255, 255, 255, 0.8), 280px 720px #ffffff,
    440px 520px rgba(255, 255, 255, 0.6), 630px 780px #ffffff, 790px 640px rgba(255, 255, 255, 0.7),
    980px 720px #ffffff, 1140px 560px rgba(255, 255, 255, 0.8), 1310px 760px #ffffff,
    1470px 640px rgba(255, 255, 255, 0.7), 1650px 710px #ffffff, 1820px 580px rgba(255, 255, 255, 0.8),
    190px 960px #ffffff, 370px 1080px rgba(255, 255, 255, 0.7), 580px 940px #ffffff,
    770px 1140px rgba(255, 255, 255, 0.8), 940px 990px #ffffff, 1120px 1160px rgba(255, 255, 255, 0.6),
    1340px 1020px #ffffff, 1530px 1210px rgba(255, 255, 255, 0.8), 1740px 1070px #ffffff,
    250px 1380px rgba(255, 255, 255, 0.8), 520px 1460px #ffffff, 830px 1390px rgba(255, 255, 255, 0.7),
    1190px 1480px #ffffff, 1490px 1410px rgba(255, 255, 255, 0.8), 1810px 1490px #ffffff;
  animation: cosmic-twinkle 6s infinite ease-in-out alternate;
}

/* 中景白色星点：带微光晕，错落呼吸 */
.stars-medium {
  width: 1.5px;
  height: 1.5px;
  border-radius: 50%;
  background: transparent;
  box-shadow:
    80px 130px rgba(255, 255, 255, 0.95), 230px 260px #ffffff, 410px 120px rgba(255, 255, 255, 0.85),
    590px 360px #ffffff, 780px 210px rgba(255, 255, 255, 0.9), 1020px 280px #ffffff,
    1240px 110px rgba(255, 255, 255, 0.85), 1420px 340px #ffffff, 1660px 180px rgba(255, 255, 255, 0.95),
    160px 500px #ffffff, 350px 660px rgba(255, 255, 255, 0.9), 560px 580px #ffffff,
    740px 780px rgba(255, 255, 255, 0.85), 920px 670px #ffffff, 1180px 770px rgba(255, 255, 255, 0.9),
    1400px 620px #ffffff, 1620px 740px rgba(255, 255, 255, 0.9), 1800px 670px #ffffff,
    270px 1010px #ffffff, 510px 1130px rgba(255, 255, 255, 0.85), 730px 1000px #ffffff,
    1040px 1100px rgba(255, 255, 255, 0.9), 1280px 1030px #ffffff, 1580px 1150px rgba(255, 255, 255, 0.85),
    390px 1350px #ffffff, 920px 1310px rgba(255, 255, 255, 0.9), 1430px 1370px #ffffff;
  animation: cosmic-twinkle-medium 4.8s infinite ease-in-out alternate;
  animation-delay: 1.4s;
}

/* 近景明亮白色星点：带微扩散白晕 */
.stars-large {
  width: 2.2px;
  height: 2.2px;
  border-radius: 50%;
  background: transparent;
  box-shadow:
    160px 70px #ffffff,
    480px 230px #ffffff,
    850px 95px #ffffff,
    1280px 200px #ffffff,
    1610px 90px #ffffff,
    240px 610px #ffffff,
    690px 710px #ffffff,
    1110px 600px #ffffff,
    1550px 690px #ffffff,
    420px 1040px #ffffff,
    1210px 1150px #ffffff,
    1680px 1000px #ffffff,
    760px 1420px #ffffff;
  filter: drop-shadow(0 0 3px rgba(255, 255, 255, 0.9)) drop-shadow(0 0 7px rgba(255, 255, 255, 0.5));
  animation: cosmic-twinkle-large 3.8s infinite ease-in-out alternate;
  animation-delay: 2.2s;
}

/* 十字微星芒恒星点 (Lens Flare Star - 纯白微晶十字) */
.star-flare {
  position: absolute;
  width: 3px;
  height: 3px;
  background: #ffffff;
  border-radius: 50%;
  box-shadow:
    0 0 8px 1px rgba(255, 255, 255, 0.95),
    0 0 18px 2px rgba(255, 255, 255, 0.4);
}

.star-flare::before,
.star-flare::after {
  content: '';
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  background: linear-gradient(to right, transparent, rgba(255, 255, 255, 0.95), transparent);
  border-radius: 9999px;
}

/* 水平射线 */
.star-flare::before {
  width: 28px;
  height: 1px;
}

/* 垂直射线 */
.star-flare::after {
  width: 1px;
  height: 28px;
  background: linear-gradient(to bottom, transparent, rgba(255, 255, 255, 0.95), transparent);
}

/* 星芒具体定位 */
.star-flare-1 {
  top: 10%;
  left: 16%;
  animation: flare-pulse 4.2s infinite ease-in-out alternate;
}

.star-flare-2 {
  top: 18%;
  right: 15%;
  animation: flare-pulse 5.4s infinite ease-in-out alternate;
  animation-delay: 1.6s;
}

.star-flare-3 {
  top: 42%;
  left: 7%;
  animation: flare-pulse 4.8s infinite ease-in-out alternate;
  animation-delay: 0.9s;
}

.star-flare-4 {
  top: 58%;
  right: 19%;
  animation: flare-pulse 6.2s infinite ease-in-out alternate;
  animation-delay: 2.4s;
}

.star-flare-5 {
  top: 78%;
  left: 28%;
  animation: flare-pulse 5.1s infinite ease-in-out alternate;
  animation-delay: 3.1s;
}

.star-flare-6 {
  top: 88%;
  right: 34%;
  animation: flare-pulse 5.8s infinite ease-in-out alternate;
  animation-delay: 1.8s;
}

/* 纯白流星划过夜空动画 (Shooting Stars) */
.shooting-star {
  position: absolute;
  height: 1.5px;
  background: linear-gradient(-45deg, rgba(255, 255, 255, 1), rgba(255, 255, 255, 0.4), transparent);
  border-radius: 999px;
  filter: drop-shadow(0 0 6px rgba(255, 255, 255, 0.8));
  transform: rotate(-40deg);
  opacity: 0;
}

.shooting-star-1 {
  top: 14%;
  left: 65%;
  width: 110px;
  animation: shooting-star-anim-1 12s ease-in-out infinite;
  animation-delay: 2.5s;
}

.shooting-star-2 {
  top: 48%;
  left: 82%;
  width: 90px;
  animation: shooting-star-anim-2 15s ease-in-out infinite;
  animation-delay: 8s;
}

@keyframes shooting-star-anim-1 {
  0% {
    transform: rotate(-38deg) translateX(0);
    opacity: 0;
  }
  4% {
    opacity: 0.9;
  }
  8% {
    transform: rotate(-38deg) translateX(-360px);
    opacity: 0;
  }
  100% {
    transform: rotate(-38deg) translateX(-360px);
    opacity: 0;
  }
}

@keyframes shooting-star-anim-2 {
  0% {
    transform: rotate(-42deg) translateX(0);
    opacity: 0;
  }
  3% {
    opacity: 0.8;
  }
  7% {
    transform: rotate(-42deg) translateX(-300px);
    opacity: 0;
  }
  100% {
    transform: rotate(-42deg) translateX(-300px);
    opacity: 0;
  }
}

/* 呼吸动画：纯亮度和光晕变化，100% 纯白与深空黑，无任何变色 */
@keyframes cosmic-twinkle {
  0% {
    opacity: 0.22;
  }
  50% {
    opacity: 0.85;
  }
  100% {
    opacity: 0.32;
  }
}

@keyframes cosmic-twinkle-medium {
  0% {
    opacity: 0.28;
    transform: scale(0.95);
  }
  50% {
    opacity: 1;
    transform: scale(1.08);
  }
  100% {
    opacity: 0.38;
    transform: scale(0.98);
  }
}

@keyframes cosmic-twinkle-large {
  0% {
    opacity: 0.35;
  }
  50% {
    opacity: 1;
  }
  100% {
    opacity: 0.45;
  }
}

@keyframes flare-pulse {
  0% {
    opacity: 0.25;
    transform: scale(0.8);
  }
  50% {
    opacity: 1;
    transform: scale(1.18);
  }
  100% {
    opacity: 0.35;
    transform: scale(0.85);
  }
}
</style>
