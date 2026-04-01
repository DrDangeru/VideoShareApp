document.addEventListener('DOMContentLoaded', () => {
  const prerollOverlay = document.getElementById('preroll-ad-overlay');
  const mainVideo = document.getElementById('main-video');
  const countdownSpan = document.getElementById('ad-countdown');

  if (prerollOverlay && mainVideo && countdownSpan) {
    // 50% chance to show a pre-roll ad
    if (Math.random() > 0.5) {
      let timeLeft = 5;

      const endAd = () => {
        prerollOverlay.style.display = 'none';
        mainVideo.play().catch(e => console.log("Auto-play blocked", e));
      };

      // Start countdown immediately
      const countdownInterval = setInterval(() => {
        timeLeft--;
        if (timeLeft > 0) {
          countdownSpan.textContent = timeLeft;
        } else {
          clearInterval(countdownInterval);
          endAd();
        }
      }, 1000);

    } else {
      // No ad, hide overlay immediately
      prerollOverlay.style.display = 'none';
    }
  }
});
