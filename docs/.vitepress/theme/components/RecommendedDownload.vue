<script setup lang="ts">
import { withBase } from 'vitepress'
defineProps<{ locale?: string }>()
</script>
<template>
  <section class="recommended-download">
    <h2>{{ locale === 'en' ? 'Yunqi service + Web console' : 'Yunqi 服务与 Web 控制台' }}</h2>
    <p>{{ locale === 'en' ? 'One service binary with an embedded browser console, or deploy with Docker.' : '下载内置浏览器控制台的服务二进制，或使用 Docker 部署。' }}</p>
    <a href="https://github.com/soulteary/yunqi/releases/latest">{{ locale === 'en' ? 'Download service binaries' : '下载服务二进制' }}</a>
    <span> · </span>
    <a :href="withBase(locale === 'en' ? '/en/guide/getting-started' : '/guide/getting-started')">{{ locale === 'en' ? 'Deployment guide' : '部署指南' }}</a>
  </section>
</template>
