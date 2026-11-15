<template>
  <svg
    class="admin-mark"
    viewBox="360 380 1320 1240"
    xmlns="http://www.w3.org/2000/svg"
    aria-hidden="true"
    focusable="false"
  >
    <polygon
      class="cls-1"
      points="928 781 1021 951 784.5 1371.97 1618 1371.97 1530.32 1544 509 1539 928 781"
    />
    <polygon
      class="cls-3"
      points="1618 1371.97 784.5 1371.97 874.93 1211 1346 1211 923.1 456 1110.06 456 1618 1371.97"
    />
    <polygon
      class="cls-2"
      points="418 1372.74 509 1539 928 781 1162.32 1211 1346 1211 923.1 456 418 1372.74"
    />
  </svg>
</template>
<style scoped>
  .admin-mark {
    display: block;
    width: 32px;
    height: 32px;
    flex: 0 0 32px;
    overflow: visible;
    animation: admin-mark-float 4s ease-in-out infinite;
  }
  .cls-1,
  .cls-2,
  .cls-3 {
    stroke-width: 0;
    fill: var(--mark-base);
    animation: admin-mark-color 4s ease infinite both;
  }
  .cls-1 {
    --mark-base: #f15a24;
    --mark-middle: #ed1c24;
    --mark-end: #f7931e;
  }
  .cls-2 {
    --mark-base: #f7931e;
    --mark-middle: #f15a24;
    --mark-end: #ed1c24;
  }
  .cls-3 {
    --mark-base: #ed1c24;
    --mark-middle: #f7931e;
    --mark-end: #f15a24;
  }
  @keyframes admin-mark-color {
    0%,
    100% {
      fill: var(--mark-base);
    }
    34% {
      fill: var(--mark-middle);
    }
    66% {
      fill: var(--mark-end);
    }
  }
  @keyframes admin-mark-float {
    0%,
    100% {
      transform: translateY(0);
    }
    50% {
      transform: translateY(1.5px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .admin-mark,
    .cls-1,
    .cls-2,
    .cls-3 {
      animation: none;
    }
  }
</style>
