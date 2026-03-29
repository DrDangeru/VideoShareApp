document.addEventListener('DOMContentLoaded', () => {
    // Ad pool logic using internal videos
    const videoAds = [
      "/Videos/VID_20260220_171545.mp4",
      "/Videos/VID_20260313_170235.mp4",
      "/Videos/VID_20260313_170322.mp4",
      "/Videos/VID_20260313_170438.mp4",
      "/Videos/VID_20260313_170539.mp4"
    ];

    const prerollOverlay = document.getElementById('preroll-ad-overlay');
    const prerollVideo = document.getElementById('preroll-video');
    const mainVideo = document.getElementById('main-video');
    const countdownSpan = document.getElementById('ad-countdown');
    const skipBtn = document.getElementById('skip-ad-btn');

    if (prerollOverlay && prerollVideo && mainVideo) {
      // 50% chance to show a pre-roll ad
      if (Math.random() > 0.5) {
        // Select random ad
        prerollVideo.src = videoAds[Math.floor(Math.random() * videoAds.length)];
        
        let timeLeft = 5;
        let countdownInterval;

        const endAd = () => {
          clearInterval(countdownInterval);
          prerollVideo.pause();
          prerollOverlay.style.display = 'none';
          mainVideo.play().catch(e => console.log("Auto-play blocked", e));
        };

        prerollVideo.onloadedmetadata = () => {
          prerollVideo.play().catch(e => {
            console.log("Ad auto-play blocked", e);
            endAd(); // If ad is blocked by browser, just skip it
          });
          
          countdownInterval = setInterval(() => {
            timeLeft--;
            if (timeLeft > 0) {
              countdownSpan.innerText = timeLeft;
            } else {
              countdownSpan.parentElement.style.display = 'none';
              skipBtn.style.display = 'block';
              clearInterval(countdownInterval);
            }
          }, 1000);
        };

        prerollVideo.onended = endAd;
        skipBtn.onclick = endAd;

      } else {
        // No ad, hide overlay
        prerollOverlay.style.display = 'none';
      }
    }
});
